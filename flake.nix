{
  description = "Financials — Rust/Axum web app";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, rust-overlay }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        overlays = [ (import rust-overlay) ];
        pkgs = import nixpkgs { inherit system overlays; };

        rustToolchain = pkgs.rust-bin.stable.latest.default;

        darwinDeps = pkgs.lib.optionals pkgs.stdenv.isDarwin [
          pkgs.apple-sdk
          pkgs.libiconv
        ];
        linuxDeps = pkgs.lib.optionals pkgs.stdenv.isLinux [
          pkgs.openssl
        ];
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            rustToolchain
            pkg-config
            sqlite
          ] ++ darwinDeps ++ linuxDeps;

          LIBSQLITE3_LIBDIR = "${pkgs.sqlite.out}/lib";
          LIBSQLITE3_INCLUDE = "${pkgs.sqlite.dev}/include";

          shellHook = ''
            echo "rustc $(rustc --version)"
            echo "cargo $(cargo --version)"
          '';
        };
      }
    );
}