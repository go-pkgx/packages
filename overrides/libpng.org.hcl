project = "libpng.org"
why     = "libpng enumerates its versions from GitHub tags and downloads from SourceForge, and the two have parted: SourceForge's libpng16 holds up to 1.6.58, GitHub carries a v1.6.59 tag, and the seed died on `GET .../libpng-1.6.59.tar.xz: 404 Not Found` for a version its own `versions:` had just offered. This adds GitHub's tag archive as a LAST mirror rather than moving the download, because SourceForge is still where releases land — and libpng commits its generated `configure`, so the archive is not a git snapshot needing autoreconf: for 1.6.58, where both exist, the two trees hold the same 611 files with nothing in either that is not in the other. The URL names pnggroup/libpng, which is where glennrp/libpng now redirects."

edits = [
  {
    path = "distributable"
    append = [
      {
        "url"              = "https://github.com/pnggroup/libpng/archive/refs/tags/v{{version}}.tar.gz"
        "strip-components" = 1
      },
    ]
  },
]
