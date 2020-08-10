# splink

A utility to read performance data from Selectronic SP PRO inverters and send to the InfluxDB time-series database.

## Installation
The utility expects the serial connection to the inverter to be available over Ethernet.  You may use a dedicated serial to Ethernet dongle (such as that sold by Selectronic).  An alternative and cheaper solution is to run [ser2net](https://github.com/I2SE/ser2net) on a host with a USB serial adapter.

Compiling this Go application should yield a static executable which can be deployed to a convenient host with access to both the serial host and the InfluxDB host.  A simple systemd unit file might look like the following:

```INI
[Unit]
Description=Interface between SP LINK protocol and InfluxDB
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/splink

[Install]
WantedBy=multi-user.target
```

## Configuration
Configuration uses the [Viper](https://github.com/spf13/viper) configuration system.  Place a file named `config.yaml`, `config.toml`, `config.json` (or other supported formats) in either the directory of the splink executable or `/etc/splink`.  Supported configuration options are:

- `host` -- the serial to Ethernet host
- `port` -- the port over which serial to Ethernet is served
- `influx_host` -- the host running InfluxDB
- `influx_port` -- the port on which InfluxDB is listening for data
- `password` -- the management password for the SP PRO
