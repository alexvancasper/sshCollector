# SSH Collector

SSH Collector is a golang app used for taking data from the network nodes over SSH protocol.
You can use it with AMQP message broker like RabbitMQ.

## Installation

Clone the repository to your PC

```bash
cd collector
chmod +x build.sh
./build.sh
```

## Usage
Make necessary changes in the config.tml file.

```bash
./collector_2.3 ./config.tml
```

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

Please make sure to update tests as appropriate.

## License
[MIT](https://choosealicense.com/licenses/mit/)
