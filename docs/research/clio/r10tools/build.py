import json,sys
H="https://help.clio.com/hc/en-us/articles/"
U={k:H+v for k,v in {
"bill":"14330519112987-Clio-Manage-Billing-Reports","client":"14344842681115-Clio-Manage-Client-Reports",
"matter":"14349646533787-Clio-Manage-Matter-Reports","pay":"14351266745755-Clio-Manage-Online-Payments-Reports",
"prod":"14351524759707-Clio-Manage-Productivity-Reports","task":"14352051388827-Clio-Manage-Task-Reports",
"rev":"14352205073051-Clio-Manage-Revenue-Reports","lsa":"14353332840603-Clio-Manage-Law-Society-of-Alberta-Trust-Report",
"pi":"16719358011547-Clio-Manage-Personal-Injury-Reports","acct":"21249768099355-Accounting-Reports",
"acctdash":"21254364296219-Accounting-Dashboard","ai":"29658606211483-Generate-Reports-and-Monitor-Performance",
"grow":"29739406189339-Clio-Grow-Reports","custom":"37685184870299-Custom-Reports-in-Clio-Manage",
"export":"50633368921243-Export-Data-From-Clio-Manage","recon":"54824841490075-View-Reconciliation-Reports",
"presets":"9290078155803-Clio-Manage-Reports-Presets-and-History","annual":"9290091783323-Clio-Manage-Detailed-Annual-Report",
"perm":"9290095965211-Reports-Permissions-and-Settings","trustrec":"9290138047899-Trust-Reconciliation-in-Clio",
"nav":"9290390462875-Navigate-Clio-Manage"}.items()}
U["au_trust"]="https://help.clio.com/hc/en-au/articles/19199218407835"
U["insights"]="https://www.clio.com/features/law-firm-insights/"
U["insights_ca"]="https://www.clio.com/ca/features/law-firm-insights/"
U["onepager"]="https://www.clio.com/wp-content/uploads/2019/11/Clio-Manages-Firm-Dashboard-One-Pager.pdf"
U["api"]="https://www.withone.ai/knowledge/clio/conn_mod_def%3A%3AGLeorUUmBmg%3A%3AmWqBVQx7SFybbMXFZcNcqA"
U["lawkpis"]="https://clio.com/app-directory/lawkpis"
U["vid1"]="https://player.vimeo.com/video/815701981"; U["vid2"]="https://player.vimeo.com/video/815702048"
CR="Reports > Classic reports > Reports > category "
F=[]
def f(name,prod,where,what,tier,src,conf="high",shots=()):
    F.append(dict(name=name,product=prod,where=where,what=what,plan_tier=tier,sources=[U[s] for s in src],screenshots=[U[s] for s in shots],confidence=conf))
M="Clio Manage"; ALL="all current plans with Reports permission (Clio says not every report is on every plan; per-report tiers not published)"
# Billing
f("Accounts receivable report",M,CR+"Billing","Outstanding amounts on approved unpaid and past-due bills per user, client and matter; reflects partial payments, interest and credit notes; drafts excluded.",ALL,["bill"])
f("Accounts receivable aging report",M,CR+"Billing","Overdue receivables bucketed 1-30, 31-60, 61-90 and 91+ days past the bill due date, for collection follow-up.",ALL,["bill"])
f("Billing history report",M,CR+"Billing","Bills open, paid and past due with status, due date, payments, credit notes, interest and balance.",ALL,["bill"])
f("Matter balance summary report",M,CR+"Billing","Per matter: receivables, unbilled work in progress and expenses in progress; optionally client and matter trust funds. Columns can be hidden/reordered.",ALL,["bill","presets"])
f("Invoice payments report",M,CR+"Billing","Bill payments and Clio Payments refunds, how each payment was allocated to bill line items (incl. overpayments) and the users responsible for those lines.",ALL,["bill"])
f("Forex gains and losses report",M,CR+"Billing","For multi-currency firms: gain or loss from the rate at bill issue vs. rate at payment, with currency totals.","firms using multiple currencies",["bill"])
# Client (incl. trust)
f("Client activity report",M,CR+"Client","Billed and unbilled time and expense entries across a client's matters (non-billable time excluded).",ALL,["client"])
f("Client ledger report",M,CR+"Client","Per client trust and operating transactions: payments, refunds, transfers and balances.",ALL,["client"])
f("Trust ledger report",M,CR+"Client","Client and matter transactions and balances across all trust accounts; Clio positions it as suitable for regulators.",ALL,["client"])
f("Trust interest calculation report",M,CR+"Client","Computes interest earned on client/matter trust funds from a rate, compounding period and date range.",ALL,["client"])
f("Trust management report",M,CR+"Client","Unbilled time and expenses vs. trust balance and the minimum trust threshold per matter, to see where retainers are running low. Also used to carry trust balances into a migration.",ALL,["client","export"])
f("Trust listing report",M,CR+"Client","Summary of trust balances for all clients with opening and closing balances.",ALL,["client"])
f("Work in progress report",M,CR+"Client","Unbilled time and expenses per matter: entries on draft or pending-approval bills or not on any bill.",ALL,["client"])
f("Bank account activity report",M,CR+"Client","Funds in and out across all firm bank accounts (operating and trust) with running balance.",ALL,["client"])
# Matter
f("Matters report",M,CR+"Matter","Matter list with description, responsible and originating attorney, last bill date, AR, billable flag, practice area, status and client contact details.",ALL,["matter"])
f("Matters by responsible attorney report",M,CR+"Matter","Matters grouped by responsible attorney with client name and description.",ALL,["matter"])
f("Contact information report",M,CR+"Matter","Clients, their matters and basic contact details; suggested for building an email list.",ALL,["matter"])
f("Billing rate report",M,CR+"Matter","Custom and default client, matter and user billing rates across matters.",ALL,["matter"])
# Productivity
f("Productivity by client report",M,CR+"Productivity","Amount billed per client and each client's share of firm billings (approved or paid bills).",ALL,["prod"])
f("Productivity by user report",M,CR+"Productivity","Per user time and expense split into unbilled (draft/pending bills), billed and total, incl. discounts.",ALL,["prod"])
f("Matter productivity by user report",M,CR+"Productivity","Each user's billed, unbilled and non-billable time and expenses grouped by matter.",ALL,["prod"])
f("Realization report",M,CR+"Productivity","Per user progression of time from original value through discounts/write-offs to billed and collected.",ALL,["prod"])
# Revenue
f("Revenue report",M,CR+"Revenue","Unbilled, billed and collected time and expenses by matter incl. taxes, credit notes, discounts and interest; results depend on creation vs. billing date.",ALL,["rev"])
f("Originating attorney revenue report",M,CR+"Revenue","Revenue brought in per originating attorney incl. taxes, interest and collected time and expenses.",ALL,["rev"])
f("Fee allocation report",M,CR+"Revenue","Per user and matter: billed and collected time and expenses, AR and credit notes, to work out compensation splits.",ALL,["rev"])
# Task
f("Task progress by user report",M,CR+"Task","Per user task counts by status (pending, in progress, in review, completed) with time estimates.",ALL,["task"])
f("Task productivity by user report",M,CR+"Task","Per user counts of upcoming, overdue and completed tasks.",ALL,["task"])
# Online payments
f("Clio Payments monthly statements",M,CR+"Online payments","Monthly statement of all online payments for bookkeeping and tax.","firms using Clio Payments",["pay"])
f("Clio Payments ledger report",M,CR+"Online payments","All payment-processor transactions by type and status (charges, fees, refunds, disputes, payouts).","firms using Clio Payments",["pay"])
f("Clio Payments transaction report",M,CR+"Online payments","Authorized payment transactions with processing fees.","firms using Clio Payments",["pay"])
f("Clio Payments deposit report",M,CR+"Online payments","Deposits (payouts) with the matching transaction fees, daily and monthly.","firms using Clio Payments",["pay"])
f("Form 1099-K report",M,CR+"Online payments","US IRS settlement report for card processing volume.","US firms using Clio Payments",["pay"])
# PI
f("Personal injury reports (damages, medical expenses, medical expense payments)",M,CR+"Personal injury","Damages and medical bills recorded on a matter; medical expenses by provider with liens and balances; payments by payer.","firms using PI matter features",["pi"])
# Compliance
f("Law Society of Alberta trust report (Trust Safety Accounting Upload)",M,"Reports > Reports > category Compliance","Alberta-specific annual trust filing output; enabled by default when the account mailing address is in Canada; cannot be preset or scheduled.","Canadian accounts (Alberta use)",["lsa","presets"])
f("Trust account management reports (Australia)",M,"Reports (AU accounts)","Jurisdiction-specific trust compliance reports incl. a file maintenance report of changes to contacts, matters and bank accounts; only for accounts set to Australia.","Australian accounts only",["au_trust"],"medium")
f("Trust reconciliation report",M,"Accounts > trust account > Reconciliations > View report","Per completed reconciliation: summary (ledger vs bank and difference), transaction details (in/out, interest, charges, adjustments, cleared and uncleared) and per-client ledger effect; PDF export; kept 10 years.","current plans only; admins reconcile",["recon","trustrec"])
# Mechanics
f("Report output formats and column chooser",M,"Classic report > Output format","Most reports output CSV and PDF; some Excel and Web. For CSV on 11 named reports, columns can be hidden, shown and reordered.",ALL,["presets"])
f("Report presets",M,"Reports > Classic reports > Presets","Save a report plus filters as a named preset; generate, edit or delete. Presets are private to their creator; not available for 1099-K, Clio Payments statements and Alberta compliance reports.","US Core/Signature/Elite; CA Essentials/Advanced/Complete",["presets"],"high",["vid1","vid2"])
f("Scheduled presets",M,"Save as preset > Add schedule","Daily, weekly (chosen days) or monthly (every 1-12 months) at a set time with an 'effective from' date; result emailed to the creator; schedules run for up to 12 months.","US Core/Signature/Elite; CA Essentials/Advanced/Complete",["presets"])
f("Report history",M,"Reports > History","Own report runs from the last six months; sort, filter by category/output/date, search by name, reopen in original format. Users cannot see others' runs.",ALL,["presets"])
f("Custom reports (Reports Beta)",M,"Reports > Reports Beta > Create new custom report","Pick a dataset, fields and calculations with live sample preview; name, description, category; refine with columns, filters (unlimited, default last month), up to two group-by levels and currency. Filtering/grouping only on standard fields, not custom fields. Still labelled Beta.","create: US Signature/Elite, CA Advanced/Expand; others can refine Clio pre-built reports",["custom","insights_ca"])
f("Custom report datasets",M,"Reports Beta > dataset picker","Account transactions (operating+trust), Bills, Calendar entries, Fee allocation, Online payments, Matters, Payments and credit notes, Time entry productivity; most include matter/contact standard and some custom fields (not contact-select, matter-select or multi-line text).","as custom reports",["custom"])
f("Report views",M,"Reports Beta > report > Save report view","Saved refinement (columns, filters, groups) of a custom or Clio report; per-report subtabs Report (key-metric summary, columns, filters, groups), Report views, History of exports with who/when/format and re-download.","unlimited on US Signature/Elite, CA Advanced/Expand; 5 per Clio report on US Core / CA Essentials",["custom"])
f("Scheduled report views",M,"Reports Beta > Report views > Add schedule","Daily/weekly/monthly with time, start date, format and recipients; higher tiers can notify other firm users, lower tiers self only.","US Core+, CA Essentials+ (others-notify on US Signature/Elite, CA Advanced/Expand)",["custom"])
f("Duplicate and customize",M,"Reports Beta > card > More","Copy any existing (incl. Clio pre-built) report as the base of a new custom report; edit, export or delete.","as custom reports",["custom"])
f("AI report building (Manage AI)",M,"Reports Beta > Manage AI prompt; AI Launchpad > Ask a question","Describe a report in plain language and the AI preselects dataset fields and calculations; ask the Launchpad to run a standard report for a period or answer personal-metric questions (billed hours, realization, collection, utilization).","firms with Clio's AI features (Manage AI add-on)",["ai","custom"])
f("Firm Dashboard",M,"Dashboard > Firm Dashboard","Utilization (vs 8-hour day), realization (billed minus discounted over billable hours) and collection rates by year, by activity date; date selector, user filter, currency picker. Visible to admins and users with reports permission.","all current plans (reports permission)",["nav","insights","onepager"],"high",["onepager"])
f("Industry benchmarks on dashboard (Legal Trends Report)",M,"Dashboard (marketing claim)","Marketing says firms can compare their metrics against Legal Trends Report benchmarks; not described in the help article.","unknown",["insights","onepager"],"low")
f("Personal Dashboard",M,"Dashboard > Personal Dashboard","Today's agenda, hourly metrics (billable vs target), firm billing metrics by bill state, financial metrics vs annual target (260-day year) and the detailed annual report.","all; billing panels need billing+reports permission",["nav","annual"])
f("Detailed annual report and personal performance targets",M,"Settings > Profile > Personal Performance; Dashboard > Personal Dashboard","User sets rate, annual billing target, working days and fiscal year end; chart compares target vs actual billable time.","all (billing+reports permission)",["annual"])
f("Firm Feed",M,"Dashboard > Firm Feed","Firm-wide audit stream of record changes (matters, contacts, payments, bills, time, permissions, documents) filterable by user, type and deleted/exported. Admin-only by default; custom role can grant it.","all; custom roles on higher tiers",["nav"])
f("Reports permission (all or nothing)",M,"Settings > Manage Users > Permissions > Reports","One checkbox grants every report; reports cannot be restricted individually and expose colleagues' productivity and rates even without billing permission.",ALL,["perm"])
f("Reports payment allocation setting",M,"Settings > Billing > Reports allocation","Admin picks how partial payments and credit notes are spread over line items (proportional, or expenses/services oldest-first variants); affects invoice payments, fee allocation, revenue, originating revenue and the dashboard collection rate.",ALL,["perm"])
f("Bulk data export",M,"Exports page (app.clio.com/export / ca.app.clio.com/export)","Admin-only Excel-ready CSV per type: calendar entries, communications, contact notes, contact related matters, contacts, expense entries, matter notes, matters, tasks, time entries. Balances come from the AR and trust management reports, not the exporter.","all; admins only",["export"])
f("Reports API (report presets)",M,"API v4 /report_presets, /report_presets/{id}/generate_report","List, create, update, delete presets and generate a report from a preset via API.","API access",["api"],"low")
f("Clio Accounting reports","Clio Accounting","Clio Accounting > Reports","Profit and loss, general ledger, balance sheet, trial balance, cash flow, operating/trust reconciliation, vendor expenses, and (Canada) a tax summary laid out by sales-tax return line numbers incl. input tax credits.","plans that include Clio Accounting",["acct"])
f("Accounting dashboard","Clio Accounting","Clio Accounting > Dashboard","Account balances with bank-feed items to review, AR total and paid/owing invoice counts, monthly operating expenses vs prior month, hard costs to invoice, two-month P&L, best-practice checklists.","plans that include Clio Accounting",["acctdash"])
f("Clio Grow reports","Clio Grow","Clio Grow > Reports","Matter revenue by lead source, campaign and referral; lead source revenue; matters added with pipeline value and conversion rate; referral report of referring contacts. Admin permission needed.","Clio Grow",["grow","perm"])
f("Third-party analytics add-ons",M,"Clio App Directory","Apps such as LawKPIs add read-only dashboards with peer benchmarks (realization, AR aging, utilization, intake conversion).","third-party",["lawkpis"],"medium")

G=[]
def g(feat,area,scr,st,prop,pri): G.append(dict(feature=feat,our_area=area,our_screen=scr,status=st,proposal=prop,priority=pri))
g("Trust management report (WIP vs trust balance vs minimum trust threshold)","trust","report-catalogue","missing","Add catalogue row Trust > 'Retainer coverage': per matter unbilled WIP + AR vs trust held vs the matter's minimum trust / evergreen threshold, with 'top-up needed' flag; agent drafts the replenishment request (human sends).","must")
g("Trust interest calculation report","trust","report-catalogue","missing","Add Trust > 'Trust interest' for separate interest-bearing trust accounts only: rate, compounding, period, allocation per client; note mixed trust interest goes to LFO, not clients (verify).","should")
g("Bank account activity report (all firm accounts)","billing","report-catalogue","partial","Add Money > 'Bank account activity': funds in/out and running balance per general and trust account, sourced from ledgers, with bank-feed match status.","should")
g("Reports payment allocation setting","billing","billing-reports","missing","Add firm setting 'payment allocation for reports' (proportional | disbursements first | fees first, oldest first) with effective date; every money report and the collection KPI shows which method it used; changing it never rewrites past runs.","should")
g("Fee allocation settings (originating/responsible/staff percentages)","billing","report-catalogue","partial","Add firm and per-matter credit split (originating %, responsible %, staff %) and a Money > 'Fee credit allocation' report that applies it to collected fees.","should")
g("Structured schedule (daily/weekly/monthly, effective-from date, expiry)","platform","reports","partial","Replace free-text 'schedule' with fields cadence (daily|weekly days|monthly every N), time, start date, end date (default 12 months), format, internal recipients; renewal reminder before expiry.","should")
g("Report views and per-report export history","platform","custom-report-builder","partial","Add 'views' (saved column/filter/group variants of any report, incl. catalogue reports) and per-report History tab (who exported, when, format, re-download); 'Duplicate and customize' action on catalogue reports.","should")
g("Custom report datasets: calendar entries, fee allocation, online payments, payments and credit notes","platform","custom-report-builder","partial","Extend dataset enum with calendar events, credit notes, fee credit allocation and online payment transactions; allow filter/group on custom fields (Clio cannot).","should")
g("Column chooser (hide/show/reorder) on standard reports","platform","report-catalogue","missing","Add 'columns' option on every catalogue report run, remembered in presets.","could")
g("Online payments reports (deposit/payout, monthly statement, ledger)","billing","report-catalogue","partial","Split 'Online payment transactions' into: processor ledger (charges, fees, refunds, disputes, payouts), deposit/payout report matched to bank deposits, monthly processor statement.","should")
g("Forex gains and losses","billing","report-catalogue","deliberately different","CAD-only firm assumption; if multi-currency is added later, add Money > 'Foreign exchange gain/loss'. Record the assumption in the spec.","could")
g("Personal injury reports (damages, medical expenses and payments, liens)","matters","report-catalogue","missing","If PI is a supported practice area: add Matters > 'Damages and medical specials' and 'Liens and subrogation' reports fed by a matter damages schedule.","could")
g("Personal dashboard and personal performance targets","time","firm-insights","partial","Add a 'My numbers' panel on the home screen: billable vs target hours, fiscal year end, working days, billed/unbilled; targets set by the user or admin (firm-insights already has targets).","should")
g("Firm Dashboard utilization/realization/collection","platform","firm-insights","covered","Add explicit definitions matching ours (available hours basis, activity-date vs bill-date) and show which basis is used.","could")
g("Legal Trends benchmarks","platform","firm-insights","deliberately different","Keep external benchmarks out; optionally allow a firm-entered target band.","could")
g("Firm Feed (firm-wide change log with deleted/exported filter)","access","agent-activity","partial","Confirm the audit view covers human edits (not only agent events) and add filters 'deleted' and 'exported'; access via role permission.","should")
g("Per-report permissions","access","report-catalogue","covered","Ours is stronger (per-report role visibility vs Clio's single all-reports permission). Add to versus page.","could")
g("AI report building and Ask a question","platform","custom-report-builder","covered","Ours already shows query and rows; add example prompts for personal metrics (my realization last quarter).","could")
g("Accounting reports (P&L, GL, balance sheet, trial balance, cash flow, vendor expenses)","billing","billing-reports","deliberately different","State in spec that the general books live in the connected accounting system; keep exports to it. Add 'HST return worksheet' mapping our HST report to GST/HST return lines incl. ITCs from recorded disbursements (verify CRA line numbers).","should")
g("Clio Grow reports: revenue by lead source/campaign, pipeline value, referral report","intake","report-catalogue","partial","Add Intake > 'Revenue by lead source' (collected fees traced to lead source/campaign), 'Pipeline value' (expected value of open leads), 'Referrers' (referring contacts with matter counts and fees).","should")
g("Bulk CSV export by data type","platform","integrations-data","covered","Ours is broader (bills, payments, trust ledgers, audit hash). Add communications and contact-matter relationships to export_entities to match Clio.","could")
g("Reports API (presets, generate report)","platform","api-and-webhooks","missing","Add API endpoints: list catalogue, run report with filters, fetch run output, manage presets; same role visibility and export logging.","could")
g("Reconciliation report retention 10 years, 3 sections, PDF","trust","report-catalogue","covered","Confirm our monthly reconciliation output has the three parts (summary, transaction detail incl. uncleared, per-client effect).","could")
g("Alberta-style jurisdiction compliance output","trust","lso-annual-report","covered","Ours targets LSO; Clio only ships Alberta. Highlight on versus page (verify Clio has no Ontario-specific report).","could")

C=[
 dict(our_claim="platform.json clio_gaps: 'Trust reports (journal, ledger, listing, dormant)' at 'Reports > Trust / Accounts'",truth="In Clio Manage (non-AU), trust reports sit in the Client category: client ledger, trust ledger, trust interest calculation, trust management, trust listing. No help article found for a dormant/unclaimed trust report or separate receipts/disbursements journals. A dedicated 'Trust reports' article exists only for Australian accounts.",sources=[U["client"],U["au_trust"]]),
 dict(our_claim="platform.json clio_gaps: 'Custom reports' at 'Reports > Custom reports'",truth="Custom reports live under Reports > Reports Beta, are still labelled Beta, and creating them needs US Signature/Elite or CA Advanced/Expand. Filter and group work only on standard fields, not custom fields.",sources=[U["custom"]]),
 dict(our_claim="platform.json clio_gaps: 'Firm dashboard / insights with benchmarks' at 'Insights / Firm dashboard'",truth="It is Dashboard > Firm Dashboard with three metrics (utilization, realization, collection) by activity date. The benchmark comparison appears only in marketing, not in the help article. There is no separate 'Insights' screen.",sources=[U["nav"],U["insights"]]),
 dict(our_claim="platform.json clio_gaps: 'Data export to CSV per data type' at 'Settings > Data export'",truth="It is a separate Exports page (app.clio.com/export), admin-only, with ten types (calendar entries, communications, contact notes, contact related matters, contacts, expenses, matter notes, matters, tasks, time). It does not cover bills or trust ledgers; balances come from reports.",sources=[U["export"]]),
 dict(our_claim="platform.json clio_gaps: 'Scheduled reports' marked 'already covered' by reports 'schedule' (free text)",truth="Clio schedules are structured (daily/weekly/monthly every 1-12, time, effective-from date, max 12 months, email notice; report views add format and other-user recipients on higher tiers). Our free-text field is only partial coverage.",sources=[U["presets"],U["custom"]]),
 dict(our_claim="platform.json clio_gaps: 'Classic reports with filters' at 'Reports > Reports'",truth="Path is Reports > Classic reports > Reports subtab > Categories (Billing, Client, Matter, Productivity, Revenue, Task, Online payments, Personal injury, Compliance).",sources=[U["presets"],U["lsa"]]),
]
SI=[dict(area="presets/scheduling",url=U["vid1"],shows="Clio video embedded in the presets article (presets and scheduling walkthrough)"),
    dict(area="history",url=U["vid2"],shows="Second Clio video embedded in the presets article"),
    dict(area="firm dashboard",url=U["onepager"],shows="2019 Clio PDF with Firm Dashboard screenshots (utilization/realization/collection); older UI"),
    dict(area="insights marketing",url=U["insights"],shows="Clio law-firm-insights product page (403 to direct fetch; seen via search)")]
OQ=["Does Clio Canada provide trust receipts and disbursements journals or a monthly trust comparison in the BL9 sense, or only trust ledger + trust listing + reconciliation report? Help articles do not name journals.",
    "Is there any Ontario/LSO-specific report in Clio (like the Alberta one)? None found in the help center search.",
    "Exact per-report plan tiers for classic reports are not published; Clio says only 'not all reports are on every plan'.",
    "The Firm Dashboard benchmark claim (Legal Trends) is marketing-only; unverified in the product.",
    "Reports API details came from a third-party mirror; official docs.developers.clio.com not opened.",
    "Classic-presets tier text lists CA 'Complete' while custom reports list CA 'Expand'; Clio's Canadian plan names may have changed. Check current pricing."]
out=dict(id="r10-reports",scope="Every Clio report by category, dashboards (firm, personal, accounting), AI/insights reporting, custom reports, presets, scheduling, history, exports and report permissions; compared with platform.json reports, report-catalogue, custom-report-builder, firm-insights and trust/billing reports.",
 clio_products=["Clio Manage","Clio Accounting","Clio Grow","Clio Payments","Manage AI"],features=F,gaps=G,corrections=C,screenshots_index=SI,open_questions=OQ,
 sources=sorted(set(U.values())))
json.dump(out,open(sys.argv[1],"w"),indent=1,ensure_ascii=False)
print(len(F),len(G),len(C),len(SI),len(out["sources"]))
