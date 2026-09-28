project = "gnu.org/readline"
why     = "readline has to link against libtinfow, not libtinfo: without it `UP` and `tgetent` are undefined at link time."

edits = [
  {
    path = "build.env.linux.LDFLAGS"
    from = "-lncursesw"
    to   = "-Wl,--no-as-needed -ltinfow -Wl,--as-needed"
  },
]
