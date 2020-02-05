# utt

[![Codacy Badge](https://api.codacy.com/project/badge/Grade/e559940e5ce54011ad035c9f5f007c3d)](https://www.codacy.com/manual/Sunmxt/utt?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=Sunmxt/utt&amp;utm_campaign=Badge_Grade) [![pipeline status](https://git.uestc.cn/Sunmxt/utt/badges/master/pipeline.svg)](https://git.uestc.cn/Sunmxt/utt/commits/master) [![coverage report](https://git.uestc.cn/Sunmxt/utt/badges/master/coverage.svg)](https://git.uestc.cn/Sunmxt/utt/commits/master)

Overlay L2/L3 network router, designed for connecting cloud network infrastructure.

### Features

- Gossip-based membership and failure detection. Completely decentralized.
- Layer-2 and Layer-3 ovarlay support.

#### Planning

- UDP Backend.
- Dynamic NAT Traversal.

- Multiples virtual networks over one set of peers (like VxLAN).

---

#### build binary

```bash
make
```

#### run test

```bash
make test
```

#### compile protobuf

```bash
make proto
```