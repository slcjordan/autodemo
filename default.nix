{ pkgs ? import <nixpkgs> {} }:
pkgs.mkShell {
  packages = [
    pkgs.go_1_23
    pkgs.go-outline
    pkgs.godef
    pkgs.golangci-lint
    pkgs.golint
    pkgs.gopkgs
    pkgs.gopls
    pkgs.gotools
    pkgs.sqlc
    pkgs.sqlite
    pkgs.hugo
  ];
}
