build:
	docker build -t cellstate/cell:latest .

run: build
	docker run --rm -it \
		--device=/dev/net/tun \
		--cap-add=NET_ADMIN \
			cellstate/cell join e5cd7a9e1c851265

vendor:
	git clone https://github.com/codegangsta/cli.git vendor/github.com/codegangsta/cli; git checkout a65b733b303f0055f8d324d805f393cd3e7a7904