# modify-history

Warp time with the magic of Git

## Quickstart

Clone this repo and run the following in the working directory:

```sh
go build .
./modify-history open "path/to/your/repo"
```

### Features
- [x] Wipes reflog entries corresponding to the interactive rebase
- [x] Adds jitter to amend timestamp
- [x] Respects the original commit's timezone
