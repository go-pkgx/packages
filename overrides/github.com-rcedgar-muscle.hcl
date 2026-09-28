project = "github.com/rcedgar/muscle"
why     = "converted from github.com-rcedgar-muscle-darwin-strip.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.script"
    set = [
      "sed -i 's/defined(__arm64__)/defined(__arm64__) || defined(__aarch64__)/' myutils.h",
      "printf '\"v%s\"\\n' \"{{version.raw}}\" > gitver.txt",
      "grep -oE 'ClCompile Include=\"[^\"]+\\.cpp\"' muscle.vcxproj | sed -E 's/.*\"([^\"]+)\"/\\1/' > sources.txt",
      "mkdir -p o ../bin",
      "while read -r s; do",
      "c++ -ffast-math -O3 -DNDEBUG -fopenmp -c -o \"o/$${s%.cpp}.o\" \"$s\"",
      "done < sources.txt",
      "case \"$(uname)\" in Darwin) LDEXTRA=\"-Wl,-headerpad_max_install_names\";; *) LDEXTRA=\"\";; esac",
      "c++ -ffast-math -O3 -fopenmp o/*.o $LDEXTRA -o ../bin/muscle",
      "case \"$(uname)\" in Darwin) /usr/bin/strip ../bin/muscle;; *) strip ../bin/muscle;; esac",
      "mkdir -p \"{{prefix}}/bin\"",
      "install -m0755 ../bin/muscle \"{{prefix}}/bin/muscle\"",
    ]
    expect = [
      "sed -i 's/defined(__arm64__)/defined(__arm64__) || defined(__aarch64__)/' myutils.h",
      "printf '\"v%s\"\\n' \"{{version.raw}}\" > gitver.txt",
      "grep -oE 'ClCompile Include=\"[^\"]+\\.cpp\"' muscle.vcxproj | sed -E 's/.*\"([^\"]+)\"/\\1/' > sources.txt",
      "mkdir -p o ../bin",
      "while read -r s; do",
      "c++ -ffast-math -O3 -DNDEBUG -fopenmp -c -o \"o/$${s%.cpp}.o\" \"$s\"",
      "done < sources.txt",
      "case \"$(uname)\" in Darwin) LDEXTRA=\"-Wl,-headerpad_max_install_names\";; *) LDEXTRA=\"\";; esac",
      "c++ -ffast-math -O3 -fopenmp o/*.o $LDEXTRA -o ../bin/muscle",
      "strip ../bin/muscle",
      "mkdir -p \"{{prefix}}/bin\"",
      "install -m0755 ../bin/muscle \"{{prefix}}/bin/muscle\"",
    ]
  },
]
