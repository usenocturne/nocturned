{
  description = "Nocturne daemon";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
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
              after = [ "network.target" ];
              
              serviceConfig = {
                ExecStart = "${self.packages.${pkgs.system}.default}/bin/nocturned";
                Environment = [ "PORT=${toString cfg.port}" ];
                DynamicUser = true;
                RuntimeDirectory = "nocturne";
                RuntimeDirectoryMode = "0755";
                StateDirectory = "nocturne";
                StateDirectoryMode = "0700";
                CacheDirectory = "nocturne";
                CacheDirectoryMode = "0750";
                Restart = "on-failure";
                
                CapabilityBoundingSet = "";
                DevicePolicy = "closed";
                NoNewPrivileges = true;
                PrivateDevices = true;
                PrivateTmp = true;
                ProtectSystem = "strict";
                ProtectHome = true;
                RestrictAddressFamilies = [ "AF_INET" "AF_INET6" ];
                RestrictNamespaces = true;
                RestrictRealtime = true;
                RestrictSUIDSGID = true;
                ProtectKernelTunables = true;
                ProtectKernelModules = true;
                ProtectControlGroups = true;
                
                ReadOnlyPaths = [ "/etc/nocturne" ];
              };
            };
          };
        };
    in
    (flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "nocturned";
          version = "1.0.0";
          src = ./.;

          vendorHash = null;

          meta = with pkgs.lib; {
            description = "Nocturne daemon";
            homepage = "https://github.com/usenocturne/nocturned";
            license = licenses.mit;
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            go-tools
          ];
        };
      })) // {
        nixosModules.default = nixosModule;
      };
}