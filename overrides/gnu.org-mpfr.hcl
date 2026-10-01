project = "gnu.org/mpfr"
why     = "The recipe's test compiles with `gcc -lgmp -lmpfr test.c -o test` — the libraries BEFORE the source. GNU ld resolves left to right, so by the time it reads test.c's object there is nothing pending for -lmpfr to satisfy, and the link fails with eight undefined references to symbols the library plainly exports. Measured on the LinuxONE runner on 2026-10-01: the bottle is GOOD — `readelf --dyn-syms libmpfr.so.6.2.2` lists 561 mpfr_ symbols including the `mpfr_get_version` and `mpfr_inits2` the linker called undefined, the file is a correct s390x ELF, and the distro's rival cannot be picked because it ships no `libmpfr.so`. The discriminator is the order alone, proven in place with LIBRARY_PATH on the bottle: `gcc -lgmp -lmpfr x.c` gives an undefined reference and `gcc x.c -lgmp -lmpfr` links. Libraries last is correct everywhere and cannot make a sound bottle fail, so if this test fails again the bottle is the reason. NOT yet understood: gnu.org/mpc writes the same shape (`cc -lgmp -lmpc -lmpfr test.c`) and PASSES on this platform — I could not find the mechanism, and it is not the compiler driver (cc and gcc both fail libs-first here) nor lld (ours cannot start at all, go-pkgx/bk#164). See go-pkgx/bk#260."

edits = [
  {
    path = "test.script"
    from = "gcc -lgmp -lmpfr test.c -o test"
    to   = "gcc test.c -lgmp -lmpfr -o test"
  },
]
