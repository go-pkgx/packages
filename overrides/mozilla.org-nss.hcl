project = "mozilla.org/nss"
why     = "nss records an absolute rpath, so its own dylibs cannot resolve nspr once installed anywhere else — bk's fixup guard refuses to publish that. It also needs -Werror relaxed and its test library left unbuilt."

edits = [
  {
    path = "build.env.darwin"
    set = {
      "ARGS" = [
        "NSS_ENABLE_WERROR=0",
      ]
    }
  },
  {
    path = "build.script"
    set = [
      {
        "run" = [
          "sed -i 's|-install_name @executable_path|-Wl,-rpath,@loader_path/../../../.. -install_name {{prefix}}/lib|g' coreconf/Darwin.mk",
          "grep -q '^DSO_LDOPTS = -bundle$' lib/ckfw/builtins/Makefile",
          "sed -i 's|^DSO_LDOPTS = -bundle$|DSO_LDOPTS = -bundle -Wl,-rpath,@loader_path/../../../..|' lib/ckfw/builtins/Makefile",
          "sed -i 's|@executable_path|{{prefix}}/lib|g' lib/freebl/config.mk",
          "make all $ARGS",
        ]
        "working-directory" = "nss"
      },
      {
        "run" = [
          "mkdir -p bin lib/pkgconfig include/dbm include/nss",
          "cat <<< \"$PC_FILE\" > lib/pkgconfig/nss.pc",
        ]
        "working-directory" = "{{prefix}}"
      },
      "cat <<< \"$CONFIG_FILE\" > ./dist/nss-config",
      "install ./dist/nss-config {{prefix}}/bin/",
      "install ./dist/$(uname)*/bin/* {{prefix}}/bin/",
      {
        "run" = <<EOT
for f in ./dist/$(uname)*/lib/*; do
  case "$${f##*/}" in *-testlib.*) continue;; esac
  install "$f" {{prefix}}/lib/
done
EOT
      },
      "install ./dist/public/dbm/* {{prefix}}/include/dbm/",
      "install ./dist/public/nss/* {{prefix}}/include/nss/",
    ]
    expect = [
      {
        "run" = [
          "sed -i 's|-install_name @executable_path|-install_name {{prefix}}/lib|g' coreconf/Darwin.mk",
          "sed -i 's|@executable_path|{{prefix}}/lib|g' lib/freebl/config.mk",
          "make all $ARGS",
        ]
        "working-directory" = "nss"
      },
      {
        "run" = [
          "mkdir -p bin lib/pkgconfig include/dbm include/nss",
          "cat <<< \"$PC_FILE\" > lib/pkgconfig/nss.pc",
        ]
        "working-directory" = "{{prefix}}"
      },
      "cat <<< \"$CONFIG_FILE\" > ./dist/nss-config",
      "install ./dist/nss-config {{prefix}}/bin/",
      "install ./dist/$(uname)*/bin/* {{prefix}}/bin/",
      "install ./dist/$(uname)*/lib/* {{prefix}}/lib/",
      "install ./dist/public/dbm/* {{prefix}}/include/dbm/",
      "install ./dist/public/nss/* {{prefix}}/include/nss/",
    ]
  },
]
