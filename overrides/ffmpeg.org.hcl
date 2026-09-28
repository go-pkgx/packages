project = "ffmpeg.org"
why     = "ffmpeg's rebuild dies before it links anything, building manual pages the bottle does not ship. Nothing consumes them and they need a texinfo the closure does not carry."

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
