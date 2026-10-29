package inspector

// DarwinPathPrelude augments PATH on macOS (Darwin) hosts for non-interactive SSH sessions
// to ensure user-installed binaries (such as Docker Desktop and Homebrew) are discoverable.
// On non-Darwin hosts (e.g. Linux), this shell snippet is a harmless no-op.
const DarwinPathPrelude = "case \"$(uname -s)\" in Darwin) PATH=\"$PATH:/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/Applications/Docker.app/Contents/Resources/bin\"; export PATH;; esac; "
