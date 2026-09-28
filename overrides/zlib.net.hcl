project = "zlib.net"
why     = "zlib.net's front page carries only the CURRENT release, so every older version 404s the moment a new one ships — while `versions:` happily lists them all from GitHub. Download where the versions are enumerated."

edits = [
  {
    path = "distributable.url"
    set  = "https://github.com/madler/zlib/releases/download/v{{version.raw}}/zlib-{{version.raw}}.tar.gz"
  },
]
