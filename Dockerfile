FROM alpine:3.6

RUN apk add --update --no-cache \
	ca-certificates

COPY virtuoso-vindu /virtuoso-vindu

CMD /virtuoso-vindu
