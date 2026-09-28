project = "gnu.org/texinfo"
why     = "texinfo's perl XS link never sees bk's LDFLAGS, so the extension is built without the rpath it needs and the result cannot find its own libraries."

edits = [
  {
    path = "build.script"
    prepend = [
      {
        "if"  = "darwin"
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
