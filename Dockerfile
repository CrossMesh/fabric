FROM alpine:3.7

RUN apk add --no-cache iproute2 net-tools bash vim;\
    mkdir -p /etc

COPY bin/utt /bin/utt
COPY utt.yml /etc/utt.yml

ENTRYPOINT ["/bin/utt"]