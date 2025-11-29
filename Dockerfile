FROM alpine:3.22.2 AS ca

RUN apk upgrade --no-cache \
	&& apk add --no-cache ca-certificates=20250911-r0 zip=3.0-r13 tzdata=2025b-r0 \
	&& addgroup -S pivot -g 9911 && adduser -S pivot -G pivot -u 9911

WORKDIR /usr/share/zoneinfo

RUN zip -r -0 /zoneinfo.zip .

FROM scratch

ENV ZONEINFO=/zoneinfo.zip
COPY --from=ca /zoneinfo.zip /

COPY --from=ca /etc/group  /etc/group
COPY --from=ca /etc/passwd /etc/passwd
COPY --from=ca /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY ./pivot /pivot

USER pivot
ENTRYPOINT ["/pivot"]
