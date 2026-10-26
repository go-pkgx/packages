project = "protobuf.dev"
why     = "This recipe names abseil.io in BOTH dependency lists with constraints that cannot both hold: `dependencies: abseil.io: \"*\"` and `build.dependencies: abseil.io: ^20260526`. bk treats the runtime list as the set a build LINKS against and the build list as the set it RUNS, so both are installed and one build gets two abseils — headers from 20260817, libraries from 20260526. Measured on darwin/aarch64 (run 40357785785): `ld: \"absl::lts_20260817::Mutex::AssertHeld() const\", referenced from: … symbol(s) not found for architecture arm64`. When the same mismatch happens to LINK instead of failing, the bottle ships an @rpath reference to the version the headers came from while declaring a constraint consumers resolve differently — that is the class behind the 2608 references in go-pkgx/packages#367. ^20260526 is upstream's own build line for protobuf 36.2, and upstream's comment says re2/protobuf/grpc must share one abseil LTS line, so matching the runtime constraint to the build one is the narrow fix rather than bumping either. See go-pkgx/bk#357. Delete this when upstream stops writing `*` here."

edits = [
  {
    path = "dependencies[\"abseil.io\"]"
    set  = "^20260526"
  },
]
