# Zapi2Tor

This is a client written in Go that allows you to easily proxy all your traffic through Tor using a webtunnel.

## ⚠️ Important Notice

This application creates a new network interface and modifies your system's DNS settings on Linux. Therefore, it is crucial to shut down the program correctly.

When you launch the application, a tray icon will appear. You can use this icon to connect to Tor, disconnect, and safely exit the application. **Failure to use the "Exit" option from the tray menu can lead to broken network settings, preventing internet access until they are restored.**

## Network Recovery

If the application terminates unexpectedly and your internet connection stops working, simply restart the application and click the "Отключить" button to restore your network settings.

## Using a Custom Bridge

For better performance and security, it is highly recommended to use your own webtunnel bridge.

1.  Obtain a bridge from the official Tor Project website: [https://bridges.torproject.org/options](https://bridges.torproject.org/options)
2.  Once you have your bridge line, insert it into the designated location in the source code.
3.  Recompile the project using the command:
    ```bash
    go build -o zapi2tor
    ```

If you skip this step, you will use a default, shared bridge, which may result in slower connection speeds.
