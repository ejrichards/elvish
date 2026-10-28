{
  description = "elvish";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, gomod2nix }:
    let
      allSystems = [
        "x86_64-linux" # 64-bit Intel/AMD Linux
        "aarch64-linux" # 64-bit ARM Linux
        "x86_64-darwin" # 64-bit Intel macOS
        "aarch64-darwin" # 64-bit ARM macOS
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs allSystems (system: f {
        inherit system;
        pkgs = import nixpkgs { inherit system; };
      });

      # Nix builds from a source archive without the .git directory, so Go's
      # VCS stamping does not work and the version would be "0.x.0-dev.unknown".
      # Supply the same "$time-$commit" string via buildinfo.VCSOverride instead.
      rev = self.rev or self.dirtyRev or "unknown";
      vcsOverride = "${self.lastModifiedDate or "19700101000000"}-${builtins.substring 0 12 rev}";

      # Mirror the version string that buildinfo.devVersion produces, so that
      # the store path name (and tools like nvd) show the same version as
      # "elvish -version". VersionBase is read from the Go source to keep the
      # two in sync.
      versionBase = builtins.elemAt
        (builtins.match ".*const VersionBase = \"([^\"]+)\".*"
          (builtins.readFile ./pkg/buildinfo/buildinfo.go)) 0;
      version = "${versionBase}-dev.0.${vcsOverride}";
    in
    {
      packages = forAllSystems ({ system, pkgs, ... }:
        let
          buildGoApplication = gomod2nix.legacyPackages.${system}.buildGoApplication;
        in
        rec {
          default = elvish;

          # The website, built by the toolchain under website/. It is a
          # separate Go module with a local replace pointing at the parent
          # tree; proxyVendor keeps that replace out of the vendor hash, so
          # the hash only changes when the website's own dependencies do.
          docsite = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
            pname = "elvish-docsite";
            inherit version;
            src = ./.;
            modRoot = "website";
            proxyVendor = true;
            vendorHash = "sha256-nckgG2fEX471IUxHHrBI91iEdZb+gS8gPfmHqrLyLcQ=";
            nativeBuildInputs = [ pkgs.gnumake ];
            doCheck = false;

            buildPhase = ''
              runHook preBuild
              make gen
              runHook postBuild
            '';

            # Symlinks are dereferenced since go:embed refuses them.
            installPhase = ''
              runHook preInstall
              mkdir -p $out
              cp -RL _dst/. $out/
              runHook postInstall
            '';
          };

          elvish = buildGoApplication {
            pname = "elvish";
            inherit version;
            src = ./.;
            go = pkgs.go_1_27;
            pwd = ./.;
            subPackages = [ "cmd/elvish" ];
            CGO_ENABLED = 0;
            flags = [
              "-trimpath"
            ];
            ldflags = [
              "-s"
              "-w"
              "-extldflags -static"
              "-X src.elv.sh/pkg/buildinfo.VCSOverride=${vcsOverride}"
              "-X src.elv.sh/pkg/buildinfo.BuildVariant=nix"
            ];

            # Embed the website so that "elvish -docs" works; this is what
            # "go generate ./pkg/docsite" does outside of Nix.
            preBuild = ''
              cp -R ${docsite}/. pkg/docsite/site/
            '';

            # Allows `users.users.<name>.shell = pkgs.elvish;` in NixOS.
            passthru.shellPath = "/bin/elvish";

            meta = with pkgs.lib; {
              description = "Expressive programming language and versatile interactive shell";
              homepage = "https://elv.sh/";
              license = licenses.bsd2;
              mainProgram = "elvish";
              platforms = platforms.unix;
            };
          };
        });

      # "nix develop" provides a shell containing development tools.
      #
      # "nix develop --command gomod2nix" should be run to update gomod2nix.toml
      # after updating Go module dependencies.
      devShell = forAllSystems ({ system, pkgs }:
        pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_27
            gomod2nix.legacyPackages.${system}.gomod2nix
            gopls
          ];
        });

      overlays.default = final: prev: {
        elvish = self.packages.${final.stdenv.system}.elvish;
      };
    };
}

