retracer: URL Redirect Tracer
=============================

`h12.me/retracer` is a Go package that can trace any HTTP(S) URL redirections regardless
they are 3xx redirections, http-eqiv refreshes, Javascript navigations and the non-standard
[HTTP Refresh header](http://www.otsukare.info/2015/03/26/refresh-http-header).

Features
--------

* Cookie, Referer and custom headers supported
* Avoid unnecessary resource loading, like: CSS, images and iframes

Install
-------

```
sudo apt-get install surf xvfb
go get h12.me/retracer
```

Tips
----

It is recommended to use Xvfb to prevent Surf window appearing during Javascript tracing.

```bash
xvfb :99 &
DISPLAY=:99 ./your_program
```

To build Surf:

```bash
sudo apt-get install libwebkitgtk-dev
make
```
