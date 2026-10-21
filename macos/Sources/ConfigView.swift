import AppKit
import SwiftUI

private struct AppConfig: Codable {
    var context = ""
    var namespace = "default"
    var envFile = ""
    var forwards: [ForwardEntry] = []

    enum CodingKeys: String, CodingKey {
        case context, namespace, forwards
        case envFile = "env_file"
    }
}

private struct ForwardEntry: Codable, Identifiable {
    var id = UUID()
    var service = ""
    var namespace = ""
    var ports: [String] = [""]

    enum CodingKeys: String, CodingKey {
        case service, namespace, ports
    }
}

struct ConfigView: View {
    @ObservedObject var controller: ForwardController
    @State private var config = AppConfig()
    @State private var message = ""

    private var isJSON: Bool {
        URL(fileURLWithPath: controller.configPath).pathExtension.lowercased() == "json"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("KubePin Configuration").font(.title2.bold())
            Text(controller.configPath)
                .font(.caption.monospaced())
                .textSelection(.enabled)
            HStack {
                Button("Choose File…", action: chooseFile)
                Button("Use App Config") {
                    controller.configPath = ForwardController.managedConfigPath
                    load()
                }
                Button("Open in Editor") {
                    NSWorkspace.shared.open(URL(fileURLWithPath: controller.configPath))
                }
                .disabled(!FileManager.default.fileExists(atPath: controller.configPath))
            }

            if isJSON {
                HStack {
                    TextField("Kubernetes context", text: $config.context)
                    TextField("Default namespace", text: $config.namespace)
                }
                TextField("Environment file (optional)", text: $config.envFile)
                Text("Relative environment file paths resolve from the config file’s directory.")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                ScrollView {
                    VStack(spacing: 10) {
                        ForEach($config.forwards) { $forward in
                            ForwardEditor(forward: $forward) {
                                config.forwards.removeAll { $0.id == forward.id }
                            }
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
                .frame(minHeight: 150)

                HStack {
                    Button("Add Service") { config.forwards.append(ForwardEntry()) }
                    Spacer()
                    Button("Save") { save() }
                        .keyboardShortcut("s", modifiers: .command)
                }
            } else {
                Text("YAML files can be run here. Choose a JSON file or Use App Config to edit services and ports in this window.")
                    .foregroundStyle(.secondary)
            }

            if !message.isEmpty {
                Text(message).font(.caption).textSelection(.enabled)
            }
            Divider()
            HStack {
                Text(controller.statusText).font(.headline)
                Spacer()
                Button("Start") { controller.start() }.disabled(controller.isRunning)
                Button("Stop") { controller.stop() }.disabled(!controller.isRunning)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(Array(controller.lines.enumerated()), id: \.offset) { entry in
                        Text(entry.element).font(.caption.monospaced())
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
            .frame(height: 100)
            .padding(6)
            .background(.quaternary, in: RoundedRectangle(cornerRadius: 6))
        }
        .padding(16)
        .frame(minWidth: 600, minHeight: 530)
        .onAppear(perform: load)
        .onChange(of: controller.configPath) { _, _ in load() }
    }

    private func chooseFile() {
        let panel = NSOpenPanel()
        panel.allowedFileTypes = ["json", "yaml", "yml"]
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = false
        if panel.runModal() == .OK, let url = panel.url {
            controller.configPath = url.path
            load()
        }
    }

    private func load() {
        message = ""
        guard isJSON else { return }
        guard FileManager.default.fileExists(atPath: controller.configPath) else {
            config = AppConfig()
            return
        }
        do {
            let data = try Data(contentsOf: URL(fileURLWithPath: controller.configPath))
            config = try JSONDecoder().decode(AppConfig.self, from: data)
        } catch {
            message = "Could not read config: \(error.localizedDescription)"
        }
    }

    private func save() {
        let context = config.context.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !context.isEmpty else { message = "A Kubernetes context is required."; return }
        guard !config.forwards.isEmpty else { message = "Add at least one service."; return }
        for entry in config.forwards {
            if entry.service.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                entry.ports.isEmpty || entry.ports.contains(where: { $0.isEmpty }) {
                message = "Each service needs a name and at least one LOCAL:REMOTE port mapping."
                return
            }
        }
        do {
            let url = URL(fileURLWithPath: controller.configPath)
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            var saved = config
            saved.context = context
            saved.namespace = config.namespace.trimmingCharacters(in: .whitespacesAndNewlines)
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
            try encoder.encode(saved).write(to: url, options: .atomic)
            message = controller.isRunning ? "Saved. Stop and start forwarding to apply changes." : "Saved."
        } catch {
            message = "Could not save config: \(error.localizedDescription)"
        }
    }
}

private struct ForwardEditor: View {
    @Binding var forward: ForwardEntry
    var onRemove: () -> Void

    private var portsText: Binding<String> {
        Binding(
            get: { forward.ports.joined(separator: ", ") },
            set: { forward.ports = $0.split(separator: ",", omittingEmptySubsequences: false)
                .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) } }
        )
    }

    var body: some View {
        GroupBox {
            VStack(spacing: 8) {
                HStack {
                    TextField("Service name", text: $forward.service)
                    TextField("Namespace (optional)", text: $forward.namespace)
                    Button("Remove", role: .destructive, action: onRemove)
                }
                TextField("Ports, e.g. 15432:5432, 15433:5433", text: portsText)
            }
            .padding(4)
        }
    }
}
