{
  description = "Nocturne daemon";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, gomod2nix }:
    let
      nixosModule = { config, lib, pkgs, ... }:
        let cfg = config.services.nocturned;
        in {
          options.services.nocturned = {
            enable = lib.mkEnableOption "Nocturne daemon";
            port = lib.mkOption {
              type = lib.types.port;
              default = 5000;
              description = "Port to listen on";
            };
          };

          config = lib.mkIf cfg.enable {
            systemd.services.nocturned = {
              description = "Nocturne daemon";
              wantedBy = [ "multi-user.target" ];
              requires = [ "dbus.service" ];
              after = [ "dbus.service" ];

              serviceConfig = {
                ExecStart = "${self.packages.${pkgs.system}.default}/bin/nocturned";
                Environment = [ "PORT=${toString cfg.port}" ];

                User = "root";
                Group = "wheel";

                Restart = "on-failure";
                RestartSec = "5s";

                # ReadOnlyPaths = [ "/etc/nocturne" ];
              };
            };
          };
        };
    in
    (flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        buildGoApplication = gomod2nix.legacyPackages.${system}.buildGoApplication;
      in
      {
        packages.default = buildGoApplication {
          name = "nocturned";
          version = "1.0.0";
          go = pkgs.go_1_22;

          src = ./.;
          pwd = ./.;

          meta = with pkgs.lib; {
            description = "Nocturne daemon";
            homepage = "https://github.com/usenocturne/nocturned";
            license = licenses.mit;
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_22
            gopls
            go-tools
            gomod2nix.packages.${system}.default
          ];
        };
      })) // {
        nixosModules.default = nixosModule;
      };
}