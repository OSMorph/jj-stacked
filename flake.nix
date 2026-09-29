{
  description = "Pinned jj-stacked development and offline validation worker";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/efef4deb458e5d0234c7ebf3b15e3abf984019a9";
    vulndb = {
      url = "github:golang/vulndb/5775cfa9da25ff717723a7bb7ea716c86f056335";
      flake = false;
    };
  };

  outputs =
    { nixpkgs, vulndb, ... }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs { inherit system; };
      lib = pkgs.lib;

      projectDepsSource = lib.fileset.toSource {
        root = ./.;
        fileset = lib.fileset.unions [
          ./cmd
          ./internal
          ./go.mod
          ./go.sum
        ];
      };

      dependencyProbe = pkgs.buildGoModule {
        pname = "jj-stacked-dependencies";
        version = "2.6.0";
        src = projectDepsSource;
        vendorHash = "sha256-vizicwBqVAzeQm5Up3KP83NUhlrjuTrBoWAuxMJ9sGQ=";
        proxyVendor = true;
        subPackages = [ "cmd/jj-stacked" ];
      };

      mkJujutsu = version: hash: pkgs.stdenvNoCC.mkDerivation {
        pname = "jujutsu";
        inherit version;
        src = pkgs.fetchurl {
          url = "https://github.com/jj-vcs/jj/releases/download/v${version}/jj-v${version}-x86_64-unknown-linux-musl.tar.gz";
          inherit hash;
        };
        sourceRoot = ".";
        installPhase = ''
          runHook preInstall
          install -Dm755 jj "$out/bin/jj-${version}"
          runHook postInstall
        '';
      };

      jj027 = mkJujutsu "0.27.0" "sha256-fccbmzvibmwRxagNM3MaT4rSjGKEDBKHGUB/Lllvnk8=";
      jj044 = mkJujutsu "0.44.0" "sha256-Cge6tGQaVf0rwv0VY7o6P5pXdYQIatdAhqHFtps//Ok=";
      jjDefault = pkgs.runCommand "jujutsu-default-0.44.0" { } ''
        mkdir -p "$out/bin"
        ln -s ${jj044}/bin/jj-0.44.0 "$out/bin/jj"
      '';

      workerTools = with pkgs; [
        bash
        coreutils
        diffutils
        findutils
        gcc
        gitMinimal
        gnugrep
        gnumake
        gnused
        go_1_26
        golangci-lint
      ];

      offlineGovulncheck = pkgs.writeShellScriptBin "govulncheck" ''
        exec ${pkgs.govulncheck}/bin/govulncheck \
          -db=file://${vulndb}/data/osv \
          "$@"
      '';

      workerEntrypoint = pkgs.writeShellScriptBin "jj-stacked-worker" ''
        set -euo pipefail

        case "''${JJ_VERSION:-}" in
          0.27.0) jj_binary=${jj027}/bin/jj-0.27.0 ;;
          0.44.0) jj_binary=${jj044}/bin/jj-0.44.0 ;;
          *)
            printf 'JJ_VERSION must be 0.27.0 or 0.44.0\n' >&2
            exit 2
            ;;
        esac

        mkdir -p /tmp/selected-bin
        ln -s "$jj_binary" /tmp/selected-bin/jj
        export PATH="/tmp/selected-bin:${offlineGovulncheck}/bin:${lib.makeBinPath workerTools}"
        export HOME=/tmp/home
        export GOCACHE=/tmp/go-build
        export GOMODCACHE=/tmp/go-mod
        export GOFLAGS=-mod=readonly
        export GOPROXY=file://${dependencyProbe.goModules}
        export GOSUMDB=off
        export GOTOOLCHAIN=local
        export GOTMPDIR=/tmp/go-tmp
        export JJ_CONFIG=/dev/null
        export LANG=C.UTF-8
        export LC_ALL=C.UTF-8
        export XDG_CACHE_HOME=/tmp/xdg-cache
        export XDG_CONFIG_HOME=/tmp/xdg-config

        mkdir -p "$HOME" "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR" "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME"
        cd /src
        exec "$@"
      '';

      workerFilesystem = pkgs.runCommand "jj-stacked-worker-filesystem" { } ''
        mkdir -p "$out/tmp" "$out/src" "$out/artifacts" "$out/etc" "$out/usr/bin"
        chmod 1777 "$out/tmp"
        ln -s ${pkgs.coreutils}/bin/env "$out/usr/bin/env"
        cat > "$out/etc/passwd" <<'EOF'
        root:x:0:0:root:/root:/bin/bash
        worker:x:1000:1000:jj-stacked worker:/tmp/home:/bin/bash
        EOF
        cat > "$out/etc/group" <<'EOF'
        root:x:0:
        worker:x:1000:
        EOF
      '';

      workerRoot = pkgs.buildEnv {
        name = "jj-stacked-worker-root";
        paths = workerTools ++ [
          jj027
          jj044
          offlineGovulncheck
          workerEntrypoint
          workerFilesystem
        ];
        pathsToLink = [
          "/bin"
          "/etc"
          "/usr"
        ];
      };

      workerImage = pkgs.dockerTools.buildLayeredImage {
        name = "localhost/jj-stacked-validation-worker";
        tag = "go1.26.6";
        created = "1970-01-01T00:00:01Z";
        contents = [ workerRoot ];
        config = {
          User = "1000:1000";
          WorkingDir = "/src";
          Entrypoint = [ "${workerEntrypoint}/bin/jj-stacked-worker" ];
          Env = [
            "PATH=${lib.makeBinPath workerTools}"
          ];
        };
      };
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = workerTools ++ [
          jjDefault
          pkgs.govulncheck
          pkgs.podman
        ];
      };

      packages.${system} = {
        inherit jj027 jj044 workerImage;
        goModules = dependencyProbe.goModules;
        default = workerImage;
      };

      checks.${system}.toolVersions = pkgs.runCommand "jj-stacked-tool-versions" {
        nativeBuildInputs = workerTools ++ [ jj027 jj044 pkgs.govulncheck ];
      } ''
        set -eu
        test "$(go version | awk '{ print $3 }')" = go1.26.6
        test "$(golangci-lint version --short)" = 2.12.2
        test "$(${jj027}/bin/jj-0.27.0 --version)" = "jj 0.27.0-6ce7a77da5a18343f4f3effef49b77428e43bc74"
        test "$(${jj044}/bin/jj-0.44.0 --version)" = "jj 0.44.0-af45d57de7163cc5f17f3df178a31577e4951ea3"
        test "$(${jjDefault}/bin/jj --version)" = "jj 0.44.0-af45d57de7163cc5f17f3df178a31577e4951ea3"
        command -v govulncheck >/dev/null
        command -v gcc >/dev/null
        command -v git >/dev/null
        touch "$out"
      '';
    };
}
