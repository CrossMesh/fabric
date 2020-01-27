# utt

[![Codacy Badge](https://api.codacy.com/project/badge/Grade/e559940e5ce54011ad035c9f5f007c3d)](https://www.codacy.com/manual/Sunmxt/utt?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=Sunmxt/utt&amp;utm_campaign=Badge_Grade)

We are searching for reliable vxlan network solution over network with limited connectivity (limited ports, limited protocols, etc). **utt** multiplexes avaliable protocols to tunnel UDP packet without packet boundary corruption, passing UDP traffic through firewall. 

#### build binary

```bash
make
```

#### run test

```bash
make test
```