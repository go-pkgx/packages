provides = [
  "bin/simdjson-minify",
]

build {
  dependencies = {
    "libcxx.llvm.org/wasi" = "*"
  }
  script = [
    {
      prop = <<EOT
#include <iostream>
#include <iterator>
#include <string>
#include "simdjson.h"
int main() {
  std::string in((std::istreambuf_iterator<char>(std::cin)), std::istreambuf_iterator<char>());
  simdjson::padded_string p(in);
  std::string out(in.size() + simdjson::SIMDJSON_PADDING, '\0');
  size_t n = 0;
  auto err = simdjson::get_active_implementation()->minify(
      reinterpret_cast<const uint8_t *>(p.data()), p.length(),
      reinterpret_cast<uint8_t *>(&out[0]), n);
  if (err) { std::cerr << simdjson::error_message(err) << "\n"; return 1; }
  std::cout.write(out.data(), n);
  std::cout << "\n";
  std::cerr << "implementation: " << simdjson::get_active_implementation()->name() << "\n";
  return 0;
}
EOT
      run = "cp $PROP minify.cpp"
    },
    {
      run = "$BK_CXX -std=c++17 -O2 -Iinclude -Isrc -o minify.wasm src/simdjson.cpp minify.cpp"
    },
    "install -D minify.wasm \"{{ prefix }}/bin/simdjson-minify.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/simdjson/simdjson/archive/refs/tags/{{ version.tag }}.tar.gz"
}

versions {
  github = "simdjson/simdjson"
}
