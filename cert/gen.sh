#!/bin/sh
set -e

openssl genrsa -out key.orig 1024
openssl req -new -key key.orig -out key.csr
openssl rsa -in key.orig -out key
openssl x509 -req -days 3650 -in key.csr -signkey key -out crt
