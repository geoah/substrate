import { ItemScreen } from "./ItemScreen"
import { ItemsScreen } from "./ItemsScreen"
import { useRoute } from "./route"
import { SettingsScreen } from "./SettingsScreen"

/** The app's three screens: the items, one item, and Settings. */
export function App() {
  const [route, go] = useRoute()
  switch (route.screen) {
    case "item":
      return <ItemScreen id={route.id} onBack={() => go({ screen: "items" })} />
    case "settings":
      return <SettingsScreen onBack={() => go({ screen: "items" })} />
    case "items":
      return (
        <ItemsScreen
          onOpen={(id) => go({ screen: "item", id })}
          onSettings={() => go({ screen: "settings" })}
        />
      )
  }
}
