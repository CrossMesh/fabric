# CrossMesh

[![Codacy Badge](https://api.codacy.com/project/badge/Grade/e559940e5ce54011ad035c9f5f007c3d)](https://www.codacy.com/manual/Sunmxt/utt?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=Sunmxt/utt&amp;utm_campaign=Badge_Grade) [![pipeline status](https://git.uestc.cn/Sunmxt/utt/badges/master/pipeline.svg)](https://git.uestc.cn/Sunmxt/utt/commits/master) [![coverage report](https://git.uestc.cn/Sunmxt/utt/badges/master/coverage.svg)](https://git.uestc.cn/Sunmxt/utt/commits/master)

**CrossMesh** 是一个覆盖网络（Overlay network）方案，用于连接云网络基础设施，构建多机房、混合云基础网络。

**CrossMesh** 目标是为容器、虚拟机、物理机、VPC跨云互联提供简单、灵活、安全且高性能的虚拟网络（Overlay）。



### 特性

- 节点管理、故障检查基于 Gossip 协议。完全去中心化。不依赖协调组件。
- 支持 Layer-2 和 Layer-3 Overlay



### 计划中的特性

- Metrics 指标提供可观测性（Observability）
- 实现虚拟网络 VPC
- UDP 协议 Backend
- Kubernetes CNI 支持
- 动态 NAT 穿透



---

## Get Started

### 构建 Binary

```bash
make
```

### 构建 RPM/SRPM 包

```bash
make rpm
make srpm
```

### 构建 Docker Image

```bash
GOOS=linux make docker
```



### 定义网络

编辑 **utt.yml** 或 **/etc/utt.yml** (如果使用 RPM 包)

```yaml
link:
  # 定义一个二层网络.
  vnet1:
    mode: ethernet
    iface:
      name: vn1
      mac: ea:38:ab:40:00:12
      address: 10.240.3.1/24 # 节点虚拟地址.
      
    backends:
    - psk: 123456
      encrypt: false
      type: tcp
      params:
        bind: 0.0.0.0:3880
        publish: 192.168.0.161:80
        priority: 1
```

### 启动网络

```bash
utt -c utt.yml edge vnet1
```

或者**使用 systemd**:

```bash
systemctl start utt-vnet@vnet1
```



### 使用 Seed 连接另一个 CrossMesh Edge

```bash
utt net seed vnet1 tcp:121.78.89.11:3880
```

### 配置完成

**CrossMesh** 自动维护网络节点之间的配对。配置已经完成。



## 开发指南

### 运行测试用例

```bash
make test
```

### 编译 Protobuf

```bash
make proto
```



