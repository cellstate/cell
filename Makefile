build:
	docker build -t cellstate/cell:latest .

run: build
	export `cat ./secrets.env`; docker run --rm -it \
		--device=/dev/net/tun \
		--cap-add=NET_ADMIN \
			cellstate/cell --token=$$ZEROTIER_TOKEN join e5cd7a9e1c266442

deps:
	rm -fr vendor
	git clone https://github.com/codegangsta/cli.git vendor/github.com/codegangsta/cli; cd vendor/github.com/codegangsta/cli; git checkout a65b733b303f0055f8d324d805f393cd3e7a7904
	git clone https://github.com/golang/net.git vendor/golang.org/x/net; cd vendor/golang.org/x/net; git checkout c764672d0ee39ffd83cfcb375804d3181302b62b
	git clone https://github.com/anacrolix/torrent vendor/github.com/anacrolix/torrent; cd vendor/github.com/anacrolix/torrent; git checkout b10c9921710fb770823d4bdb2025601c8a4daa17