# bootstrap
Bootstrap is used to symlink dotfiles into the correct location.

[![GoDoc](https://godoc.org/github.com/dangerhuss/bootstrap?status.svg)](https://godoc.org/github.com/dangerhuss/bootstrap)

## Usage
Bootstrap searches for json files named `links.json` under the dotfile source directory specified in the `$DOT` environment variable or the `--dry` command line option.

The `links.json` file should contain a dictionary of `"source": "destination"` pairs. The destinaion path can contain environment variables.

```
bootstrap --help
Usage of bootstrap:
  -dir string
        The dotfiles source. (default "/Users/dangerhuss/src/dotfiles")
  -dry
        Only print out the changes.
  -force
        Overwrite existing links.
```

## Example
This example shows how to link your source controlled `.zshrc` to `$HOME/.zshrc`

```shell
$ export DOT="$HOME/src/dotfiles"
$ mkdir -p $DOT/zsh && cd $DOT/zsh
$ touch zshrc.zsh # create a zshrc file using the zsh filetype
$ cat links.json
{
        "zshrc.zsh": "$HOME/.zshrc"
}
$
$ bootstrap --dry
ls -s /Users/dangerhuss/src/dotfiles/zsh/zshrc.zsh /Users/dangerhuss/.zshrc
Changes will take effect after sourcing your .*shrc-force
$
$ bootstrap
/Users/dangerhuss/go/src/github.com/dangerhuss/bootstrap/zshrc.zsh -> /Users/dangerhuss/.zshrc
Changes will take effect after sourcing your .*shrc
```

