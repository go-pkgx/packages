project = "videolan.org/x265"
why     = "converted from videolan.org-x265-build.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.script"
    set = [
      {
        "if" = "<4"
        "run" = <<EOT
python3 - <<'EOF'
import glob, re
changed = []
for p in glob.glob("source/**/CMakeLists.txt", recursive=True):
    s = open(p).read()
    before = s
    s = re.sub(r'if\(POLICY (CMP0025|CMP0054)\)\n *cmake_policy\(SET \1 OLD\)[^\n]*\n *endif\(\)\n', '', s)
    s = re.sub(r'(cmake_minimum_required *\( *VERSION *)(2(?:\.\d+)*)', r'\g<1>3.5', s)
    s = s.replace('$${CMAKE_CXX_COMPILER_ID} STREQUAL "Clang"', '$${CMAKE_CXX_COMPILER_ID} MATCHES "Clang"')
    if s != before:
        open(p, "w").write(s)
        changed.append(p)
assert changed, "nothing changed — the shapes this patch targets are gone"
print("patched:", " ".join(changed))
EOF
EOT
        "working-directory" = ".."
      },
      {
        "run" = <<EOT
cmake ../source -DENABLE_HDR10_PLUS=ON $ARGS_DEFAULT $HIGHBITARGS
make
mv libx265.a ../8bit/libx265_main10.a
EOT
        "working-directory" = "../10bit"
      },
      {
        "run" = <<EOT
cmake ../source -DMAIN12=ON $ARGS_DEFAULT $HIGHBITARGS
make
mv libx265.a ../8bit/libx265_main12.a
EOT
        "working-directory" = "../12bit"
      },
      {
        "run" = <<EOT
cmake ../source $ARGS_DEFAULT $ARGS
make
mv libx265.a libx265_main.a
EOT
      },
      {
        "if" = "darwin"
        "run" = "/usr/bin/libtool -static -o $LIB_ARGS"
      },
      {
        "if" = "linux"
        "run" = "ar crs $LIB_ARGS"
      },
      {
        "run" = "make install"
      },
    ]
    expect = [
      {
        "run" = <<EOT
cmake ../source -DENABLE_HDR10_PLUS=ON $ARGS_DEFAULT $HIGHBITARGS
make
mv libx265.a ../8bit/libx265_main10.a
EOT
        "working-directory" = "../10bit"
      },
      {
        "run" = <<EOT
cmake ../source -DMAIN12=ON $ARGS_DEFAULT $HIGHBITARGS
make
mv libx265.a ../8bit/libx265_main12.a
EOT
        "working-directory" = "../12bit"
      },
      {
        "run" = <<EOT
cmake ../source $ARGS_DEFAULT $ARGS
make
mv libx265.a libx265_main.a
EOT
      },
      {
        "if" = "darwin"
        "run" = "libtool -static -o $LIB_ARGS"
      },
      {
        "if" = "linux"
        "run" = "ar crs $LIB_ARGS"
      },
      {
        "run" = "make install"
      },
    ]
  },
]
