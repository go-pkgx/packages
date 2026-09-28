project = "lloyd.github.io/yajl"
why     = "yajl asks for a `cmake_minimum_required` below 3.5, which CMake 4 removed, AND uses GET_TARGET_PROPERTY(... LOCATION), which CMake 4 removed with policy CMP0026. The policy flag alone is not enough: the source has to say $<TARGET_FILE:...> instead."

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
