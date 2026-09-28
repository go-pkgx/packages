project = "github.com/util-linux/util-linux"
why     = "converted from github.com-util-linux-util-linux-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
