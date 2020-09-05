# utt
[![Codacy Badge](https://app.codacy.com/project/badge/Grade/a4f9ba98642d4f9798d02642471731ce)](https://www.codacy.com/gh/CrossMesh/fabric?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=CrossMesh/fabric&amp;utm_campaign=Badge_Grade) [![Build Status](https://travis-ci.com/crossmesh/fabric.svg?branch=master)](https://travis-ci.com/crossmesh/fabric) [![codecov](https://codecov.io/gh/crossmesh/fabric/branch/master/graph/badge.svg)](https://codecov.io/gh/crossmesh/fabric)

[简体中文](README-CN.md)

Overlay L2/L3 network router, designed for connecting cloud network infrastructure.

**UTT** focuses on flexibility, simplicity, security and performance, and could be used to connect existing resources such as bare-metals, VMs, containers, virtual switches, VPC, etc.



### Features

- Gossip-based membership and failure detection. Completely decentralized.
- Layer-2 and Layer-3 ovarlay support.

#### Planning

- Metrics.
- Multiples virtual networks over one set of peers (like VxLAN).

- UDP Backend.
- Kubernetes CNI.
- Dynamic NAT Traversal.

---

## Get Started

### build binary

```bash
make
```

### build RPM/SRPM

```bash
make rpm    # RPM
make srpm   # Source RPM
```

### build docker image

```bash
GOOS=linux make docker
```



### Define your network

Edit **utt.yml** (**/etc/utt.yml** if OS software package is used)

```yaml
link:
  # Layer 2 network for example.
  vnet1:
    mode: ethernet
    iface:
      name: vn1
      mac: ea:38:ab:40:00:12
      address: 10.240.3.1/24 # peer IP.
      
    backends:
    - psk: 123456
      encrypt: false
      type: tcp
      params:
        bind: 0.0.0.0:3880
        publish: 192.168.0.161:80
        priority: 1
```

### Start network

```bash
utt -c utt.yml edge vnet1
```

or **start with systemd**:

```bash
systemctl start utt-vnet@vnet1
```

or **start with docker**:

```bash
docker run -it --cap-add NET_ADMIN --device=/dev/net/tun utt:latest edge vnet1
```

### Seed to Publish yourself

run inside the container or on host machine.

```bash
utt net seed vnet1 tcp:121.78.89.11:3880
```

### Enjoy

**UTT** maintains membership among peers automatically. You have done settings. So enjoy it.



## Development Guide

#### run test

```bash
make test
```

#### compile protobuf

```bash
make proto
```

