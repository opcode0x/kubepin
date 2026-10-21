import Foundation

@main
struct ConfigModelTests {
    static func main() throws {
        let input = Data(#"{"context":"dev","forwards":[{"service":"redis","ports":["16379:6379"]}]}"#.utf8)
        let config = try JSONDecoder().decode(AppConfig.self, from: input)
        precondition(config.context == "dev")
        precondition(config.namespace == "default")
        precondition(config.envFile.isEmpty)
        precondition(config.forwards.count == 1)
        precondition(config.forwards[0].namespace.isEmpty)

        let output = try JSONEncoder().encode(config)
        let object = try JSONSerialization.jsonObject(with: output) as! [String: Any]
        let forwards = object["forwards"] as! [[String: Any]]
        precondition(forwards[0]["id"] == nil)
    }
}
