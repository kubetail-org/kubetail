# RipGrep for Kubernetes Logs (RGKL)

Rust package that uses [RipGrep](https://github.com/BurntSushi/ripgrep) to search Kubernetes log files

## Overview

The rgkl package implements Kubernetes log search by using RipGrep as a library to perform grep on a file-by-file basis. The rgkl code parses log files and uses RipGrep to find matching lines.

## Test

```console
cargo test
```
