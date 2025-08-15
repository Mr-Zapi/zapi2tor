package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/vishvananda/netlink"
)

//go:embed guard.png
var iconData []byte

const (
	pidFile = "/tmp/tor-vpn-helper.pid"
)

var (
	torCmd             *exec.Cmd
	tun2socksCmd       *exec.Cmd
	originalGateway    *netlink.Route
	serverRoute        *netlink.Route
	originalResolvConf = "/tmp/vpn_original_dns.conf"
	logFilePath        string
	appDir             string
)

// --- Точка входа ---
func main() {
	homeDir, _ := os.UserHomeDir()
	logFilePath = filepath.Join(homeDir, "tor-vpn-app.log")
	appDir = filepath.Join(homeDir, ".tor-vpn-app")

	if err := extractEmbeddedFiles(appDir); err != nil {
		log.Fatalf("Не удалось извлечь встроенные файлы: %v", err)
	}

	if len(os.Args) > 1 {
		if os.Geteuid() != 0 {
			log.Fatal("Помощник должен быть запущен с правами root.")
		}
		handleHelperMode(os.Args)
	} else {
		os.Remove(logFilePath)
		logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
		if err != nil {
			log.SetOutput(os.Stderr)
		} else {
			log.SetOutput(logFile)
		}
		systray.Run(onReady, onExit)
	}
}

// --- Режим GUI ---
func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("Tor VPN")
	systray.SetTooltip("Tor VPN")

	mConnect := systray.AddMenuItem("Подключить", "Запустить VPN")
	mDisconnect := systray.AddMenuItem("Отключить", "Остановить VPN и очистить сеть")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Выход", "Отключиться и закрыть приложение")

	go func() {
		for {
			select {
			case <-mConnect.ClickedCh:
				log.Println("[GUI] Нажата кнопка 'Подключить'")
				exePath, _ := os.Executable()
				cmd := exec.Command("pkexec", exePath, "--connect")
				if err := cmd.Start(); err != nil { // Запускаем в фоне и не ждем
					log.Printf("[GUI] Ошибка запуска подключения: %v", err)
				}

			case <-mDisconnect.ClickedCh:
				log.Println("[GUI] Нажата кнопка 'Отключить'")
				exePath, _ := os.Executable()
				cmd := exec.Command("pkexec", exePath, "--disconnect")
				if err := cmd.Run(); err != nil { // Ждем завершения, чтобы быть уверенными
					log.Printf("[GUI] Ошибка отключения: %v", err)
				}
				log.Println("[GUI] Команда на отключение выполнена.")

			case <-mQuit.ClickedCh:
				log.Println("[GUI] Нажата кнопка 'Выход'")
				exePath, _ := os.Executable()
				cmd := exec.Command("pkexec", exePath, "--disconnect")
				cmd.Run() // Выполняем отключение перед выходом
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {}

// --- Режим Помощника ---
func handleHelperMode(args []string) {
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		return
	}
	log.SetOutput(logFile)

	command := args[1]
	switch command {
	case "--connect":
		log.Println("[Helper] Запуск... ")
		ioutil.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			log.Println("[Helper] Сигнал завершения, очистка...")
			teardownNetworking()
			stopProcesses()
			os.Remove(pidFile)
			os.Exit(0)
		}()
		if err := startAll(); err != nil {
			log.Printf("[Helper] КРИТИЧЕСКАЯ ОШИБКА ЗАПУСКА: %v", err)
			teardownNetworking()
			stopProcesses()
			os.Remove(pidFile)
			os.Exit(1)
		}
		log.Println("[Helper] Успешно запущен. Ожидание сигнала на остановку.")
		select {}
	case "--disconnect":
		log.Println("[Helper] Остановка... ")
		if !isHelperRunning() {
			log.Println("[Helper] Процесс-помощник не найден.")
			// Все равно пытаемся почистить сеть
			teardownNetworking()
			return
		}
		pidBytes, _ := ioutil.ReadFile(pidFile)
		var pid int
		fmt.Sscanf(string(pidBytes), "%d", &pid)
		proc, err := os.FindProcess(pid)
		if err == nil {
			log.Printf("[Helper] Отправка сигнала SIGTERM процессу %d", pid)
			proc.Signal(syscall.SIGTERM)
		}
		// Ждем до 5 секунд, пока PID-файл не исчезнет
		for i := 0; i < 5; i++ {
			if !isHelperRunning() {
				log.Println("[Helper] Процесс успешно остановлен.")
				return
			}
			time.Sleep(1 * time.Second)
		}
		log.Println("[Helper] Процесс не остановился штатно. PID-файл мог остаться.")
	}
}

func startAll() error {
	log.Println("[Helper] Сохранение исходных настроек сети...")
	if err := saveOriginalState(); err != nil {
		return fmt.Errorf("ошибка сохранения исходного состояния сети: %w", err)
	}

	if err := createTorrc(); err != nil {
		return fmt.Errorf("ошибка создания torrc: %w", err)
	}

	log.Println("[Helper] Запуск Tor...")
	torPath := filepath.Join(appDir, "tor")
	torrcPath := filepath.Join(appDir, "torrc-webtunnel")
	torCmd = exec.Command(torPath, "-f", torrcPath)
	torStdout, _ := torCmd.StdoutPipe()
	torCmd.Stderr = torCmd.Stdout
	if err := torCmd.Start(); err != nil {
		return fmt.Errorf("ошибка запуска Tor: %w", err)
	}

	bootstrapped := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(torStdout)
		for scanner.Scan() {
			line := scanner.Text()
			log.Println("[Tor]:", line)
			if strings.Contains(line, "Bootstrapped 100%") {
				bootstrapped <- true
			}
		}
	}()

	select {
	case <-bootstrapped:
		log.Println("[Helper] Tor подключился.")
	case <-time.After(2 * time.Minute):
		return fmt.Errorf("Tor не смог подключиться за 2 минуты")
	}

	log.Println("[Helper] Запуск tun2socks...")
	tun2socksPath := filepath.Join(appDir, "tun2socks")
	tun2socksCmd = exec.Command(tun2socksPath, "-device", "tun://mytun", "-proxy", "socks5://127.0.0.1:9050")
	tun2socksCmd.Stdout = log.Writer()
	tun2socksCmd.Stderr = log.Writer()
	if err := tun2socksCmd.Start(); err != nil {
		return fmt.Errorf("ошибка запуска tun2socks: %w", err)
	}

	log.Println("[Helper] Настройка сети...")
	if err := setupNetworking("mytun"); err != nil {
		return fmt.Errorf("ошибка настройки сети: %w", err)
	}

	return nil
}

func stopProcesses() {
	if tun2socksCmd != nil && tun2socksCmd.Process != nil {
		tun2socksCmd.Process.Signal(syscall.SIGTERM)
	}
	if torCmd != nil && torCmd.Process != nil {
		torCmd.Process.Signal(syscall.SIGTERM)
	}
	log.Println("[Helper] Команды на остановку процессов отправлены.")
}

func createTorrc() error {
	vpnClientPath := filepath.Join(appDir, "vpn-client")
	torrcContent := fmt.Sprintf(`
UseBridges 1
DNSPort 127.0.0.1:53
AutomapHostsOnResolve 1
ClientTransportPlugin webtunnel exec %s
Bridge webtunnel [2001:db8:75db:c6f2:1dae:121:7a04:9e9d]:443 4B673DF159CFC12AC91FC2E6AC3047FF2183FCEA url=http://freifunk.ckgc.de/xBKEzZunnc3A5pcf6jaeVyPL
`, vpnClientPath)
	return ioutil.WriteFile(filepath.Join(appDir, "torrc-webtunnel"), []byte(strings.TrimSpace(torrcContent)), 0644)
}

func saveOriginalState() error {
	originalDNS, err := ioutil.ReadFile("/etc/resolv.conf")
	if err == nil {
		ioutil.WriteFile(originalResolvConf, originalDNS, 0644)
	}

	routes, err := netlink.RouteGet(net.ParseIP("8.8.8.8"))
	if err != nil || len(routes) == 0 {
		return fmt.Errorf("не удалось выполнить RouteGet: %w", err)
	}
	originalGateway = &routes[0]
	return nil
}

func setupNetworking(tunDevice string) error {
	time.Sleep(2 * time.Second)
	tunLink, err := netlink.LinkByName(tunDevice)
	if err != nil {
		return fmt.Errorf("не удалось найти интерфейс %s: %w", tunDevice, err)
	}

	if err := netlink.LinkSetUp(tunLink); err != nil {
		return fmt.Errorf("не удалось включить интерфейс %s: %w", tunDevice, err)
	}

	serverIP := net.ParseIP("92.205.186.124")
	serverRoute = &netlink.Route{
		Dst:      &net.IPNet{IP: serverIP, Mask: net.CIDRMask(32, 32)},
		Gw:       originalGateway.Gw,
		LinkIndex: originalGateway.LinkIndex,
	}
	if err := netlink.RouteReplace(serverRoute); err != nil {
		return fmt.Errorf("ошибка ЗАМЕНЫ маршрута для сервера: %w", err)
	}

	newDefaultRoute := &netlink.Route{LinkIndex: tunLink.Attrs().Index, Dst: &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}}
	if err := netlink.RouteReplace(newDefaultRoute); err != nil {
		netlink.RouteDel(serverRoute)
		return fmt.Errorf("ошибка ЗАМЕНЫ маршрута по умолчанию: %w", err)
	}

	return ioutil.WriteFile("/etc/resolv.conf", []byte("nameserver 127.0.0.1"), 0644)
}

func teardownNetworking() error {
	log.Println("[Helper] Восстановление сети...")
	if dns, err := ioutil.ReadFile(originalResolvConf); err == nil {
		ioutil.WriteFile("/etc/resolv.conf", dns, 0644)
		os.Remove(originalResolvConf)
	}

	if originalGateway != nil {
		netlink.RouteReplace(originalGateway)
	}
	if serverRoute != nil {
		netlink.RouteDel(serverRoute)
	}
	return nil
}

func isHelperRunning() bool {
	pidBytes, err := ioutil.ReadFile(pidFile)
	if err != nil { return false }
	var pid int
	fmt.Sscanf(string(pidBytes), "%d", &pid)
	proc, err := os.FindProcess(pid)
	if err != nil { return false }
	return proc.Signal(syscall.Signal(0)) == nil
}
