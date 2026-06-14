{
  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/*.tar.gz";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      flake-utils,
      nixpkgs,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        go = pkgs.go_1_25;
        formatter = pkgs.nixfmt-tree;
      in
      {
        # CI devShell — minimal toolset for go test / golangci-lint / nix flake check.
        # Kept lean so the GHA Nix cache restore is fast.
        devShells.ci = pkgs.mkShellNoCC {
          packages = [
            go
            pkgs.gnumake
            pkgs.git
            pkgs.protobuf
            pkgs.protoc-gen-go
            pkgs.protoc-gen-go-grpc
            pkgs.golangci-lint
            pkgs.actionlint
            formatter
          ];

          shellHook = ''
            export GOPATH=''${GOPATH:-$HOME/go}
            export PATH=$GOPATH/bin:$PATH
          '';
        };

        # Default devShell — adds editor / Nix language server tooling on top of CI.
        devShells.default = pkgs.mkShellNoCC {
          inputsFrom = [ (pkgs.mkShellNoCC { }) ];
          packages = [
            go
            pkgs.gnumake
            pkgs.git
            pkgs.protobuf
            pkgs.protoc-gen-go
            pkgs.protoc-gen-go-grpc
            pkgs.golangci-lint
            pkgs.actionlint
            pkgs.nil
            pkgs.goreleaser
            formatter
          ];

          shellHook = ''
            export GOPATH=''${GOPATH:-$HOME/go}
            export PATH=$GOPATH/bin:$PATH
          '';
        };

        inherit formatter;
      }
    );
}
