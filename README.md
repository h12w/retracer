retracer: URL Redirect Tracer
=============================

`h12.me/retracer` is a Go package that can trace any URL redirections regardless
they are 3xx redirections or Javascript navigations (by Surf browser).

Install
-------

```
sudo apt-get install surf xvfb
go get h12.me/retracer
```

Tips
----

It is recommended to use Xvfb to avoid Window splashes when retracer is trying
to trace a Javascript navigation by surf.

```bash
xvfb :99 &
DISPLAY=:99 ./your_program
```
