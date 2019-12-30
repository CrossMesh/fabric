# utt

We are searching for reliable vxlan network solution over network with limited connectivity (limited ports, limited protocols, etc). **utt** multiplexes avaliable protocols to tunnel UDP packet without packet boundary corruption, passing UDP traffic through firewall. 

#### build binary

```bash
make
```

#### run test

```bash
make test
```