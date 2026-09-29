project = "rust-lang.org"
why     = "rust pins `llvm.org: 21` as its build dependency, with upstream's own comment `# as of 1.91.0` dating the pin. `21` is a caret range, so it means major 21 and nothing else, and our s390x registry holds 23.1.2 — the seed died on `no version of llvm.org satisfies \"21\" (available: 1)`. rust 1.98's bootstrap asks for `major >= 21` and panics with `bad LLVM version: {version}, need >=21` (src/bootstrap/src/core/build_steps/llvm.rs), so the range is what is stale, not the LLVM."

edits = [
  {
    path   = "build.dependencies[\"llvm.org\"]"
    set    = ">=21"
    expect = 21
  },
]
