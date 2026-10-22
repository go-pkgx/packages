project = "vitejs.dev"
why     = "The recipe caps node at `^20`, and our node 20 line CANNOT RUN here: the published nodejs.org v20.20.2 bottle is linked against libcrypto.so.1.1, and every openssl bottle in this registry is 3.x or 4.x, so `npm install` dies with `node: error while loading shared libraries: libcrypto.so.1.1`. Measured 2026-10-21 on a rebuild of vitejs.dev 8.3.5, both linux arches. vite 8's own package.json says `engines.node: ^20.19.0 || >=22.12.0`, so the `^16 || ^18` half of the recipe's constraint is wrong for this version anyway, and `>=22.12` is the half that both vite accepts and this registry can satisfy — nodejs.org 26.8.1, on openssl 4.0.2. Delete this when the pantry retargets the pin or when a node 20 built against openssl 3 is published."

edits = [
  {
    path = "dependencies[\"nodejs.org\"]"
    set  = ">=22.12"
  },
]
