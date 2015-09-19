build:
	docker build -t cellstate/cell:latest .

vendor:
	git clone https://github.com/codegangsta/cli.git vendor/github.com/codegangsta/cli; git checkout a65b733b303f0055f8d324d805f393cd3e7a7904

test-cell-a: build
	docker run --rm -it --name cell-a -p 3838:3838 cellstate/cell agent

test-cell-b:
	docker run --rm --name cell-b -it -p 3800:3838 cellstate/cell agent --join "`docker inspect --format '{{ .NetworkSettings.IPAddress }}' cell-a`"