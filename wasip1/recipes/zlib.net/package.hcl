provides = [
  "bin/minigzip",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    "./configure --prefix=\"{{ prefix }}\" --static",
    "make --jobs {{ hw.concurrency }} libz.a minigzip",
    "make install",
    "install -D minigzip \"{{ prefix }}/bin/minigzip.wasm\"",
  ]

  env {
    CFLAGS = "-include errno.h -O2"
  }
}

distributable {
  strip-components = 1
  url = "https://zlib.net/zlib-{{version.raw}}.tar.gz"
}

versions {
  github = "madler/zlib"
}
