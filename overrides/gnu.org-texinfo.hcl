project = "gnu.org/texinfo"
why     = "converted from gnu.org-texinfo-xs-rpath.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.script"
    prepend = [
      {
        "if" = "darwin"
        "run" = <<EOT
mkdir -p "$SRCROOT/.bk-cc"
printf '#!/bin/sh\nexec /usr/bin/cc -Wl,-headerpad_max_install_names -Wl,-rpath,{{pkgx.prefix}} "$@"\n' > "$SRCROOT/.bk-cc/cc"
chmod +x "$SRCROOT/.bk-cc/cc"
export PATH="$SRCROOT/.bk-cc:$PATH"
export CC="$SRCROOT/.bk-cc/cc"
EOT
      },
    ]
  },
]
