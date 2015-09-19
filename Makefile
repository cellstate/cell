build:
	docker build -t cellstate/cell:latest .

vendor:
	git clone https://github.com/codegangsta/cli.git vendor/github.com/codegangsta/cli; git checkout a65b733b303f0055f8d324d805f393cd3e7a7904

test: build
	docker run --rm -it cellstate/cell