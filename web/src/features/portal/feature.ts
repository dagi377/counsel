import type { Feature } from "../../app/features";
import PortalMatters from "./PortalMatters";

const feature: Feature = {
  id: "portal",
  title: "Client portal",
  path: "/portal/matters",
  component: PortalMatters,
  surface: "portal",
};

export default feature;
