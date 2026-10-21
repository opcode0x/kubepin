import Foundation

struct AppConfig: Codable {
    var context = ""
    var namespace = "default"
    var envFile = ""
    var forwards: [ForwardEntry] = []

    enum CodingKeys: String, CodingKey {
        case context, namespace, forwards
        case envFile = "env_file"
    }

    init() {}

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        context = try values.decodeIfPresent(String.self, forKey: .context) ?? ""
        namespace = try values.decodeIfPresent(String.self, forKey: .namespace) ?? "default"
        envFile = try values.decodeIfPresent(String.self, forKey: .envFile) ?? ""
        forwards = try values.decodeIfPresent([ForwardEntry].self, forKey: .forwards) ?? []
    }
}

struct ForwardEntry: Codable, Identifiable {
    var id = UUID()
    var service = ""
    var namespace = ""
    var ports: [String] = [""]

    enum CodingKeys: String, CodingKey {
        case service, namespace, ports
    }

    init() {}

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        service = try values.decodeIfPresent(String.self, forKey: .service) ?? ""
        namespace = try values.decodeIfPresent(String.self, forKey: .namespace) ?? ""
        ports = try values.decodeIfPresent([String].self, forKey: .ports) ?? []
    }
}
