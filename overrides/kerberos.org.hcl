project = "kerberos.org"
why     = "krb5's own configure adds a large warning set and promotes several of it to errors, and one of them now fails on krb5's own source under the sovereign clang: `ccbase.c:213:8: error: assigning to 'char *' from 'const char *' discards qualifiers [-Werror,-Wincompatible-pointer-types-discards-qualifiers]` on `cp = strchr(name, ':')` — the sub-warning is covered by the `-Werror=incompatible-pointer-types` that configure itself passes. Those flags come from configure, not from the recipe, so appending to CFLAGS would be overridden by them. krb5 provides the opt-out itself: src/aclocal.m4 reads `krb5_ac_warn_cflags_set=$${WARN_CFLAGS+set}` (line 451) and then `if test \"x$krb5_ac_warn_cflags_set\" = xset ; then AC_MSG_NOTICE(not adding extra gcc warning flags because WARN_CFLAGS was set)` (line 527), so a DEFINED WARN_CFLAGS — empty is defined — makes configure skip its whole extra warning set. That is upstream's supported way to say 'do not add your warnings', rather than fighting individual flags or patching the source. Delete this when upstream's source stops discarding the qualifier."

edits = [
  {
    path   = "build.env.ARGS"
    append = ["WARN_CFLAGS="]
  },
]
