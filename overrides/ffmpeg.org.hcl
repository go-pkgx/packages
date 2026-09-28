project = "ffmpeg.org"
why     = "converted from ffmpeg.org-disable-doc.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.env.ARGS"
    set = [
      "--prefix=\"{{prefix}}\"",
      "--disable-doc",
      "--enable-libfreetype",
      "--enable-libmp3lame",
      "--enable-shared",
      "--enable-libx264",
      "--enable-gpl",
      "--enable-libx265",
      "--enable-libvpx",
      "--enable-libopus",
      "--enable-libwebp",
    ]
    expect = [
      "--prefix=\"{{prefix}}\"",
      "--enable-libfreetype",
      "--enable-libmp3lame",
      "--enable-shared",
      "--enable-libx264",
      "--enable-gpl",
      "--enable-libx265",
      "--enable-libvpx",
      "--enable-libopus",
      "--enable-libwebp",
    ]
  },
]
