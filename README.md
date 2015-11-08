# Cellstate
A decentralized solution for replicating files across an distributed system using [Git](http://git-scm.com/) and [Gossip](https://en.wikipedia.org/wiki/Gossip_protocol). It is specifically designed for high-latency networks of unstable nodes in which segmentation (P) is common, it chooses availability (A) over consistency (C). *Cellstate* aims to be easy to operate by shipping as a single [Docker](https://docker.com) container that can be deployed homogenously across all nodes in the system. New state is deployed from a workstation by using the familiar `git push` command.


## Getting Started
*Cellstate* ships currently only ships as a Docker container so you'll need to have access to a [Docker host](https://docs.docker.com/installation/) and have a Git client installed (v1.8+).

1. Open a terimnal window and start the Cellstate daemon without any arguments to initialize the gossip pool:
  
  ```
  $ docker run --name=node-one cellstate/cell
  cell: Starting Cellstate...
  cell: listening to '172.168.31.1:3838'
  ```

2. Open a second terminal and start a another node, join the gossip by using the `--join` option and point to the first instance:

   ```
   $ docker run --name=node-two cellstate/cell --join 172.168.31.1
	cell: Starting Cellstate, joining...
	cell: Joined gossip successfully
	cell: Listening to '172.168.31.2:3838'
   ```

3. Initialize a git repository into an empty directory and add _one_ of the Cellstate nodes as a remote:

   ```
   $ mkdir ~/my-data
   $ cd ~/my-data
   $ git init
   $ git remote add cellstate git@172.168.31.1:my-data
   ```

4. Create the file you would to distribute and commit it to the repository:

	```
	$ echo '{"hello": "world"}' > greetings.json
	$ git add greetings.json
	$ git commit -m "version one of my data"
	```
5. To start distributing the data simply push the commit to the cellstate remote:

	```
	$ git push cellstate master
	```

6. Upon completing the `git push`, the receiving node will now gossip the presence of a new version of the data. Other members will then start pulling the data from the first node until the new version is replicated to all nodes. To verify this happend, read the pushed data from the second node:

	```
	$ docker exec node-two cat ~/.cellstate/my-data/greetings.json
	{"hello": "world"}
	```

## Roadmap
- **Conflict Resolution and Merge Strategies:** In a distributed systems that choose avalability over consistency  it possible that different data is committed similtatenously and requires a merge. The current implementation uses the default Git merging strategy that makes no assumptions about the purpose of the data and often fails to merge without human intervention. By providing merge strategies for certain applications it is possible to reduce this problem.

- **Streamline Binary file distribution:** An important goal of
the *Cellstate* project is to fully support (large) binary files. Git isn't specifically suited for this out of box so and would require custom merge strategies (see above) and a Bittorrent-like protocol to enable pulling large files from multiple sources simultaneously.

- **Expose HTTP endpoint for webhooks:** It would be nice to edit your data right into Github and use webhooks to signal to the cluster that new data is available. This would also allow for a (more) persistent copy of the data to be always on renowned services like Bitbucket and GitHub.

- **Tagging and Branches:** Not all data needs to be replicated to all nodes all the time, tagging nodes and bundle selection logic with certain data would allow carefully control over ressilience and speed.

- **Events:** When new data is distributed throughout the system it is often required for running processes to parse the modified data. An event hook that triggers after new data is pulled to a node would allow for complex interactions with other software.

- **Monitoring:** It can be challenging to analyse how data is distributed throughout the system, a specialysed monitoring service that uses the memberlist would make it easy to notice and debug (replication) errors.

- **Using Git c-bindings:** Cellstate currently wraps the command line interface of Git, using the c-bindings would improve the performance

## Interesting Projects
There are several interesting project in the space of decentralized data (replication)

- IPFS: [https://ipfs.io/](https://ipfs.io/)
- dat data: [http://dat-data.com/](http://dat-data.com/)
- git lfs: [https://git-lfs.github.com/](https://git-lfs.github.com/)
- bup: [https://github.com/bup/bup](https://github.com/bup/bup)
- irmin [https://github.com/mirage/irmin](https://github.com/mirage/irmin)
- camlistore [http://camlistore.org/](http://camlistore.org/)

Other usefull links and articles:

- [Why git is bad with large files& projects](http://stackoverflow.com/questions/17888604/git-with-large-files)
- [Bup design document](https://raw.githubusercontent.com/bup/bup/master/DESIGN)
- [Gophers talking about hashes&syncing](https://groups.google.com/forum/#!topic/golang-nuts/ZiBcYH3Qw1g)
- [camlistore, lfs& rollsum](https://github.com/github/git-lfs/issues/355)
- [updating large Merkle Tree](http://crypto.stackexchange.com/questions/9198/efficient-incremental-updates-to-large-merkle-tree)
