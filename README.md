# Cijitter 

Cijitter is a crytomining defense tool, which consist of two components: 
- gVisor: a modified Docker container runtime, which is built on top of [gVisor](https://github.com/google/gvisor)
- monitor: a kernel module using to profile the memory access of a task

## Usage
---

### Requirements
- Linux ubuntu 5.4.0-70-generic
- Docker Engine - Community 19.03.12 or greater
- gcc (Ubuntu 7.5.0-3ubuntu1~18.04) 7.5.0

### Building docker runtime
Build and install the runsc binary:

```sh
cd gvisor && bash auto-make.sh
```
if there is any build question, check out the Building Documentation of [gVisor](https://github.com/google/gvisor) project.

### Building moniter
Build the kernel module:

```sh
mv monitor /
cd /monitor && make
```

### Using Cijitter 
To run the Cijitter, you can use `--runtime runsc-delay`:
```sh
docker run -it --runtime runsc-delay -m <memory size> <image>
```

> Note: the runsc only provide 2G memory for container by default, `-m` is required for assign larger memory for container.























