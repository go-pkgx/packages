project = "gnu.org/libtasn1"
why     = "libtasn1 probes for the FreeBSD spelling `pthread_set_name_np` by COMPILING a call to it, which only means anything if an undeclared function is an error — and under the compiler here it is a warning, so the probe succeeds everywhere and the link then fails."

edits = [
  {
    path = "build.env"
    set = {
      "CFLAGS" = "$CFLAGS -Werror=implicit-function-declaration"
    }
  },
]
