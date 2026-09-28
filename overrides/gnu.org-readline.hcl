project = "gnu.org/readline"
why     = "converted from gnu.org-readline-link-tinfow.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.env.linux.LDFLAGS"
    from = "lncursesw"
    to   = "Wl,--no-as-needed -ltinfow -Wl,--as-needed"
  },
]
