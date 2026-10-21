import Foundation

@MainActor
final class ForwardController: ObservableObject {
    @Published private(set) var statusText = "Stopped"
    @Published private(set) var isRunning = false
    @Published private(set) var lines: [String] = []
    @Published var configPath: String {
        didSet { UserDefaults.standard.set(configPath, forKey: "configPath") }
    }

    static let managedConfigPath: String = {
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return support.appendingPathComponent("KubePin/config.json").path
    }()

    private var process: Process?
    private var outputPipe: Pipe?
    private var pendingOutput = ""

    init() {
        configPath = UserDefaults.standard.string(forKey: "configPath") ?? Self.managedConfigPath
    }

    func start() {
        guard !isRunning else { return }
        guard FileManager.default.fileExists(atPath: configPath) else {
            statusText = "Choose or save a config in Settings"
            return
        }
        guard let cli = Bundle.main.url(forResource: "kubepin", withExtension: nil) else {
            statusText = "Bundled kubepin CLI is missing"
            return
        }

        let task = Process()
        task.executableURL = cli
        task.arguments = ["-config", configPath]
        task.currentDirectoryURL = URL(fileURLWithPath: configPath).deletingLastPathComponent()
        var environment = ProcessInfo.processInfo.environment
        environment["PATH"] = ["/opt/homebrew/bin", "/usr/local/bin", environment["PATH"] ?? "/usr/bin:/bin"].joined(separator: ":")
        task.environment = environment

        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = pipe
        pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            if data.isEmpty {
                handle.readabilityHandler = nil
                return
            }
            let text = String(decoding: data, as: UTF8.self)
            DispatchQueue.main.async { self?.appendOutput(text) }
        }
        task.terminationHandler = { [weak self] finished in
            DispatchQueue.main.async {
                guard let self, self.process === finished else { return }
                self.process = nil
                self.outputPipe = nil
                self.isRunning = false
                self.statusText = "Stopped (exit \(finished.terminationStatus))"
            }
        }

        do {
            try task.run()
            process = task
            outputPipe = pipe
            pendingOutput = ""
            lines = []
            isRunning = true
            statusText = "Starting…"
        } catch {
            pipe.fileHandleForReading.readabilityHandler = nil
            statusText = "Could not start: \(error.localizedDescription)"
        }
    }

    func stop() {
        guard let task = process else { return }
        process = nil
        isRunning = false
        statusText = "Stopped"
        if task.isRunning { task.terminate() }
        outputPipe?.fileHandleForReading.readabilityHandler = nil
        outputPipe = nil
    }

    private func appendOutput(_ chunk: String) {
        pendingOutput += chunk
        let parts = pendingOutput.components(separatedBy: .newlines)
        pendingOutput = parts.last ?? ""
        for line in parts.dropLast() where !line.isEmpty {
            lines.append(line)
            if lines.count > 100 { lines.removeFirst(lines.count - 100) }
            if line.contains("reconnecting in") {
                statusText = "Reconnecting…"
            } else if line.contains("Forwarding from") {
                statusText = "Forwarding"
            }
        }
    }
}
