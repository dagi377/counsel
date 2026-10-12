// The Counsel web UI kit. Screens import from "../../ui" (this file) and nothing deeper.
// Importing it also loads the stylesheet. See README.md in this directory for the rules.
import "./index.css";

export { Button, type ButtonProps, type ButtonVariant } from "./Button";
export { Field, Input, Textarea, type FieldProps, type FieldControlProps, type InputProps, type TextareaProps } from "./Field";
export { Select, type SelectOption, type SelectProps } from "./Select";
export { DataTable, type Column, type DataTableProps, type Sort, type SortDir, type TableState } from "./DataTable";
export { Menu, type MenuItem, type MenuProps } from "./Menu";
export { PageHeader, type Crumb, type PageHeaderProps } from "./PageHeader";
export { SectionCard, type SectionCardProps } from "./SectionCard";
export { FactsGrid, type Fact, type FactsGridProps } from "./FactsGrid";
export { StatusPill, STATUS, GLYPH, statusLabel, statusTone, type Status, type StatusPillProps, type Tone } from "./StatusPill";
export { Banner, type BannerProps, type BannerTone } from "./Banner";
export { EmptyState, type EmptyKind, type EmptyStateProps } from "./EmptyState";
export { Dialog, type DialogProps } from "./Dialog";
export { Tabs, TabPanel, type Tab, type TabsProps, type TabPanelProps } from "./Tabs";
export { ToastProvider, useToast, type ToastApi, type ToastInput, type ToastTone } from "./Toast";
export { Skeleton, Loading, type SkeletonProps, type LoadingProps } from "./Skeleton";

export { Money, type MoneyProps, type MoneyContext } from "./primitives/Money";
export { DateTime, type DateTimeProps } from "./primitives/DateTime";
export { formatDate, isoDate, zoneParts, ZONE, TimeError, type DateStyle } from "./primitives/time";
export { GateStatus, GateError, type GateState, type GateStatusProps, type Principal, type PersonPrincipal } from "./primitives/GateStatus";
export { MatterChip, type MatterChipProps } from "./primitives/MatterChip";

export { ListPage, type ListPageProps } from "./templates/ListPage";
export { DetailPage, type DetailPageProps } from "./templates/DetailPage";
export { SettingsPage, type SettingsPageProps, type SettingsSection } from "./templates/SettingsPage";

export { applyTheme, initTheme, readThemePref, type ThemePref } from "./theme/theme";
export { light, dark, themes, type ColourTokens, type ThemeName } from "./theme/tokens";
