retracer: URL Redirect Tracer
=============================

`h12.me/retracer` is a Go package that can trace any HTTP(S) URL redirections regardless
they are 3xx redirections, http-eqiv refreshes or Javascript navigations.

HTTP Refresh header is not supported because it is a proprietary behavior not specified in HTTP standard.

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
