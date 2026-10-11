import Quickshell
import Quickshell.Wayland
import QtQuick

ShellRoot {
    PanelWindow {
        implicitWidth: 640
        implicitHeight: 270
        color: "transparent"
        WlrLayershell.layer: WlrLayer.Overlay
        WlrLayershell.namespace: "logis-qml-bench"
        Rectangle {
            anchors.fill: parent
            radius: 14
            border.width: 2
            border.color: "#b31b1b"
            gradient: Gradient {
                GradientStop { position: 0.0; color: "#f0140d0b" }
                GradientStop { position: 1.0; color: "#f0201410" }
            }
            Column {
                x: 16; y: 16; spacing: 6
                Repeater {
                    model: ["theme  porpl", "mouse-autohide-toggle  off", "do-not-disturb  on", "keyboard-layout  us", "power-profile  balanced", "night-light  off"]
                    Text { text: modelData; color: "#e8dcc8"; font.family: "monospace"; font.pixelSize: 15 }
                }
            }
        }
    }
}
