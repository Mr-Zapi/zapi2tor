# Zapi2Tor

This is a client written in Go that allows you to easily proxy all your traffic through Tor using a webtunnel.

## ⚠️ Important Notice

This application creates a new network interface and modifies your system's DNS settings on Linux. Therefore, it is crucial to shut down the program correctly.

When you launch the application, a tray icon will appear. You can use this icon to connect to Tor, disconnect, and safely exit the application. **Failure to use the "Exit" option from the tray menu can lead to broken network settings, preventing internet access until they are restored.**

## Manual Network Recovery

If the application terminates unexpectedly, you will need to restore your network connection manually. Execute the following commands in sequence:

```bash
sudo killall -9 zapi2tor
echo "nameserver 1.1.1.1" | sudo tee /etc/resolv.conf
sudo ip route del <your-bridge-ip>
sudo ip route del default
sudo ip route add default via <your-gateway-ip> dev <your-network-interface>
sudo ip link set mytun down
```
**Note:** You will need to replace `<your-bridge-ip>`, `<your-gateway-ip>`, and `<your-network-interface>` with your actual network configuration values.

## Using a Custom Bridge

For better performance and security, it is highly recommended to use your own webtunnel bridge.

1.  Obtain a bridge from the official Tor Project website: [https://bridges.torproject.org/options](https://bridges.torproject.org/options)
2.  Once you have your bridge line, insert it into the designated location in the source code.
3.  Recompile the project using the command:
    ```bash
    go build zapi2tor
    ```

If you skip this step, you will use a default, shared bridge, which may result in slower connection speeds.
