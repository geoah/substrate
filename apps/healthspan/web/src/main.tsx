import { SubstrateApp, ToastHost } from "@substrate/react"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./App"
import { ModeProvider, readMode } from "./data/mode"
import { Import, Profile } from "./kinds"
import { manifest } from "./manifest"
import "./index.css"

// `?demo` runs on an invented member with no substrate at all; otherwise
// the connect screen until a session is kept, then the owner's own data.
const mode = readMode(window.location.search)

const icon = (
  <img
    src={`${import.meta.env.BASE_URL}icon-512.png`}
    alt=""
    className="size-24 rounded-[22%] shadow-md"
  />
)

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ModeProvider mode={mode}>
      {mode.demo ? (
        <ToastHost>
          <App />
        </ToastHost>
      ) : (
        <SubstrateApp
          manifest={manifest}
          kinds={[Profile, Import]}
          kept="health records"
          tagline="Your labs, your age score and your health record, read from your own Substrate."
          icon={icon}
        >
          <App />
        </SubstrateApp>
      )}
    </ModeProvider>
  </StrictMode>
)
