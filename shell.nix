{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  buildInputs = with pkgs; [
    go
    zig
    git
    curl

    # raylib/GLFW dependencies
    wayland
    wayland-protocols
    wayland-scanner
    libxkbcommon
    xorg.libX11
    xorg.libXcursor
    xorg.libXrandr
    xorg.libXrender
    xorg.libXinerama
    xorg.libXi
    xorg.libXext
    xorg.libXfixes
    xorg.xorgproto
    libGL
    mesa
  ];
}
