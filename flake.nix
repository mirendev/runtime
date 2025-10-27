{
  description = "A development shell for Miren Runtime";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  inputs.systems.url = "github:nix-systems/default";
  inputs.flake-utils = {
    url = "github:numtide/flake-utils";
    inputs.systems.follows = "systems";
  };
  inputs.dagger.url = "github:dagger/nix";
  inputs.dagger.inputs.nixpkgs.follows = "nixpkgs";

  outputs = {
    nixpkgs,
    flake-utils,
    dagger,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go_1_24
            pkgs.golangci-lint
            pkgs.gotools
            dagger.packages.${system}.dagger
          ];

          # Allow gopls to work in e2e tests
          GOFLAGS = "-tags=e2e";
        };
      }
    );
}
