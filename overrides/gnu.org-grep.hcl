project = "gnu.org/grep"
why     = "Building GNU grep needs a working GNU grep: its own configure refuses every other one, macOS's BSD grep included, with `configure: error: no working 'grep' found`. bk keeps a package's own bottle out of the BASE toolchain of its own build (go-pkgx/bk#105), so grep has to ask for itself here. Scaffolding, not policy."

edits = [
  {
    path = "build.dependencies[\"gnu.org/grep\"]"
    set  = "*"
  },
]
