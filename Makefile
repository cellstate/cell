build:
	GO15VENDOREXPERIMENT=1 go build -o ${GOPATH}/bin/cell main.go

vendor:
	git clone https://github.com/codegangsta/cli.git vendor/github.com/codegangsta/cli; git checkout a65b733b303f0055f8d324d805f393cd3e7a7904