import { translate as swt } from "@/i18n/runtime";
import packageJson from "../../package.json";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "ARTEX",
  version: packageJson.version,
  copyright: `© ${currentYear}, ARTEX — original project by Autumn-27.`,
  meta: {
    get title() { return swt("interface.m2410"); },
    get description() { return swt("interface.m2411"); },
  },
};
