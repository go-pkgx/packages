project = "lloyd.github.io/yajl"
why     = "converted from the unified diff overrides; the reason is in the git history"

edits = [
  {
    path = "build.env.CMAKE_ARGS"
    set = [
      "-DCMAKE_INSTALL_PREFIX=\"{{prefix}}",
      "-DCMAKE_INSTALL_LIBDIR=lib",
      "-DCMAKE_BUILD_TYPE=Release",
      "-DCMAKE_FIND_FRAMEWORK=LAST",
      "-DCMAKE_VERBOSE_MAKEFILE=ON",
      "-Wno-dev",
      "-DCMAKE_POLICY_VERSION_MINIMUM=3.5",
      "-DBUILD_TESTING=OFF",
    ]
    expect = [
      "-DCMAKE_INSTALL_PREFIX=\"{{prefix}}",
      "-DCMAKE_INSTALL_LIBDIR=lib",
      "-DCMAKE_BUILD_TYPE=Release",
      "-DCMAKE_FIND_FRAMEWORK=LAST",
      "-DCMAKE_VERBOSE_MAKEFILE=ON",
      "-Wno-dev",
      "-DBUILD_TESTING=OFF",
    ]
  },
  {
    path = "build.script"
    prepend = [
      {
        "run" = <<EOT
python3 - <<'EOF'
import re
for d, tgt in (("reformatter", "json_reformat"), ("verify", "json_verify")):
    p = d + "/CMakeLists.txt"
    s = open(p).read()
    before = s
    s = re.sub(r'^GET_TARGET_PROPERTY\(binPath ' + tgt + r' LOCATION\)\n', '', s, flags=re.M)
    s = s.replace('$${binPath}', '$<TARGET_FILE:' + tgt + '>')
    assert s != before, p
    open(p, 'w').write(s)
EOF
EOT
      },
    ]
  },
]
