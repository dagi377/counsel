/* Shared demo data. Fictional firm records; every module seeds its own collections with App.seed(). */
window.SEED = {
  firm: { name: "Linda K Law", city: "Toronto, Ontario", today: "2026-10-09" },
  users: [
    { id: "u-linda", name: "Linda Kassim", title: "Principal lawyer", role: "owner", initials: "LK", signatory: true },
    { id: "u-yoni", name: "Yonathan Degu", title: "Lawyer", role: "lawyer", initials: "YD", signatory: true },
    { id: "u-ayo", name: "Ayo Ewebiyi", title: "Associate", role: "associate", initials: "AE" },
    { id: "u-meraf", name: "Meraf Abay", title: "Paralegal", role: "paralegal", initials: "MA" },
    { id: "u-kaleab", name: "Kaleab Bereket", title: "Law clerk", role: "clerk", initials: "KB" },
    { id: "u-yohannes", name: "Yohannes Sinke", title: "Bookkeeper", role: "bookkeeper", initials: "YS" },
    { id: "u-dawit", name: "Dawit Alemu", title: "Office manager (system admin)", role: "admin", initials: "DA" },
    { id: "u-grace", name: "Grace Mendel", title: "Client (portal)", role: "client", initials: "GM", contactId: "c-grace" },
  ],
  contacts: [
    { id: "c-hanna", kind: "person", name: "Hanna Tesfaye", email: "hanna.tesfaye@example.ca", phone: "416-555-0142", idStatus: "verified" },
    { id: "c-okafor", kind: "person", name: "Samuel and Ruth Okafor", email: "okafor.family@example.ca", phone: "647-555-0188", idStatus: "verified" },
    { id: "c-fortier", kind: "person", name: "Daniel Fortier", email: "d.fortier@example.ca", phone: "905-555-0117", idStatus: "identified" },
    { id: "c-grace", kind: "person", name: "Grace Mendel", email: "grace.mendel@example.ca", phone: "416-555-0199", idStatus: "verified" },
    { id: "c-tsion", kind: "person", name: "Tsion Haile", email: "tsion.h@example.ca", phone: "437-555-0123", idStatus: "verified" },
    { id: "c-harbour", kind: "org", name: "Harbourview Bakery Inc.", email: "owner@harbourview.example.ca", phone: "416-555-0150", idStatus: "verified" },
    { id: "c-selam", kind: "person", name: "Selam Mekonnen", email: "selam.m@example.ca", phone: "416-555-0171", idStatus: "verified" },
    { id: "c-elm", kind: "person", name: "Peter Vasquez", email: "p.vasquez@example.ca", phone: "416-555-0104", idStatus: "verified" },
    { id: "c-northgate", kind: "org", name: "Northgate Storage Inc.", role: "opposing party" },
    { id: "c-osei", kind: "person", name: "Daniel Osei", org: "Brampton & Vale LLP", role: "opposing counsel", email: "dosei@bramptonvale.example.ca" },
    { id: "c-lakeshore", kind: "org", name: "Lakeshore Transit Corp.", role: "opposing party" },
    { id: "c-nglog", kind: "org", name: "Northgate Logistics Inc.", role: "opposing party" },
    { id: "c-orion", kind: "org", name: "Orion Signs Ltd.", role: "opposing party" },
  ],
  matters: [
    { id: "m1", number: "2026-0142", name: "Tesfaye v. Northgate Storage", area: "Civil litigation", clientId: "c-hanna", opposing: ["c-northgate", "c-osei"],
      responsible: "u-linda", team: ["u-linda", "u-ayo", "u-meraf"], stage: "Pleadings", status: "open", opened: "2026-09-02", fee: "hourly",
      summary: "Client's stored inventory was destroyed by a roof leak at the defendant's facility. Claim for $86,400 in damages. Statement of claim served 2026-09-23." },
    { id: "m2", number: "RE-2026-031", name: "Purchase of 14 Birchmount Rd", area: "Real estate", clientId: "c-okafor", opposing: [],
      responsible: "u-yoni", team: ["u-yoni", "u-kaleab"], stage: "Requisitions", status: "open", opened: "2026-09-15", fee: "flat",
      summary: "Residential purchase, $1,145,000, closing 2026-10-30. Mortgage with a major bank; title search ordered." },
    { id: "m3", number: "2026-0160", name: "Fortier v. Lakeshore Transit", area: "Personal injury", clientId: "c-fortier", opposing: ["c-lakeshore"],
      responsible: "u-ayo", team: ["u-ayo", "u-linda", "u-meraf"], stage: "Intake complete", status: "open", opened: "2026-10-05", fee: "contingency",
      summary: "Client fell on a wet platform at a transit station on 2026-08-14. Soft-tissue injuries; physiotherapy ongoing." },
    { id: "m4", number: "2026-0155", name: "Estate of Margaret Lindqvist", area: "Estates", clientId: "c-grace", opposing: [],
      responsible: "u-linda", team: ["u-linda", "u-meraf"], stage: "Probate application", status: "open", opened: "2026-08-20", fee: "hourly",
      summary: "Grace Mendel is estate trustee under a 2019 will. Estate value approx. $640,000. Application for certificate of appointment in preparation." },
    { id: "m5", number: "2026-0151", name: "Haile v. Northgate Logistics", area: "Employment", clientId: "c-tsion", opposing: ["c-nglog"],
      responsible: "u-ayo", team: ["u-ayo", "u-linda"], stage: "Demand", status: "open", opened: "2026-09-10", fee: "hourly",
      summary: "Warehouse supervisor of six years terminated without cause on 2026-08-29; offered two weeks' pay. Seeking common-law reasonable notice." },
    { id: "m6", number: "2026-0120", name: "Harbourview Bakery lease review", area: "Commercial", clientId: "c-harbour", opposing: [],
      responsible: "u-yoni", team: ["u-yoni"], stage: "Review", status: "open", opened: "2026-07-28", fee: "flat",
      summary: "Review of a 10-year commercial lease renewal with a rent abatement dispute." },
    { id: "m7", number: "2025-0311", name: "Mekonnen v. Orion Signs", area: "Civil litigation", clientId: "c-selam", opposing: ["c-orion"],
      responsible: "u-linda", team: ["u-linda"], stage: "Discoveries", status: "open", opened: "2025-11-03", fee: "hourly", restricted: true, wall: ["u-kaleab"],
      summary: "Breach of contract claim over a signage installation. Restricted matter: Kaleab Bereket is screened (prior work for the opposing party)." },
    { id: "m8", number: "2025-0290", name: "Purchase of 22 Elm St", area: "Real estate", clientId: "c-elm", opposing: [],
      responsible: "u-yoni", team: ["u-yoni", "u-kaleab"], stage: "Closed", status: "closed", opened: "2025-10-01", closed: "2026-01-15", fee: "flat",
      summary: "Closed residential purchase. Retention until 2036-01-15." },
  ],
};
/* Shared documents: pages are arrays of lines, so any module can cite {doc, page, line}. */
window.SEED.docs = [
  { id: "d-claim", matterId: "m1", name: "Statement of Claim", kind: "pleading", added: "2026-09-18", by: "u-linda", pages: [
    ["ONTARIO SUPERIOR COURT OF JUSTICE", "BETWEEN: HANNA TESFAYE, Plaintiff, and NORTHGATE STORAGE INC., Defendant", "STATEMENT OF CLAIM", "1. The plaintiff claims damages of $86,400 for loss of stored inventory.", "2. The plaintiff rented unit 214 at the defendant's facility at 1180 Finch Ave W since March 2024.", "3. On or about June 3, 2026, water entered unit 214 through a failed roof membrane.", "4. The defendant had notice of roof leaks from at least April 2026 and failed to repair them."],
    ["5. The inventory destroyed included 140 cartons of imported textiles valued at $86,400.", "6. The defendant breached the storage agreement and its duty of care as bailee.", "7. The plaintiff proposes that this action be tried in Toronto.", "Date: September 18, 2026", "LINDA K LAW, Lawyer for the Plaintiff"] ] },
  { id: "d-aos", matterId: "m1", name: "Affidavit of Service", kind: "affidavit", added: "2026-09-24", by: "u-meraf", pages: [
    ["AFFIDAVIT OF SERVICE", "I, Kaleab Bereket, of the City of Toronto, MAKE OATH AND SAY:", "1. On September 23, 2026 at 10:40 a.m. I served Northgate Storage Inc. with the Statement of Claim", "2. by leaving a copy with Ms. J. Park, office manager, at 1180 Finch Ave W, Toronto.", "3. Service was personal service on a corporation under rule 16.02(1)(c).", "SWORN before me on September 24, 2026"] ] },
  { id: "d-email-osei", matterId: "m1", name: "Email from D. Osei re extension", kind: "email", added: "2026-10-08", by: "agent", pages: [
    ["From: Daniel Osei, Brampton & Vale LLP", "Date: October 8, 2026", "Re: Tesfaye v. Northgate Storage", "We have just been retained by Northgate Storage.", "We ask for your consent to a 30-day extension to deliver the statement of defence.", "We also request copies of the storage agreement and the inventory invoices."] ] },
  { id: "d-aps", matterId: "m2", name: "Agreement of Purchase and Sale", kind: "contract", added: "2026-09-15", by: "u-yoni", pages: [
    ["AGREEMENT OF PURCHASE AND SALE (Residential)", "Buyer: Samuel Okafor and Ruth Okafor", "Seller: Marlon and Ines Cardoso", "Property: 14 Birchmount Rd, Toronto", "Purchase price: $1,145,000. Deposit: $57,250 to the listing brokerage.", "Requisition date: October 16, 2026."],
    ["Completion date: October 30, 2026.", "Chattels included: fridge, stove, washer, dryer.", "Seller warrants that the property is not subject to any work orders.", "Buyer's lawyer: Linda K Law."] ] },
  { id: "d-will", matterId: "m4", name: "Last Will of Margaret Lindqvist", kind: "will", added: "2026-08-20", by: "u-linda", pages: [
    ["LAST WILL AND TESTAMENT OF MARGARET LINDQVIST", "1. I revoke all prior wills.", "2. I appoint my niece GRACE MENDEL as Estate Trustee.", "3. I give my residence at 41 Willow Cres to my nephew ERIK LINDQVIST.", "4. I give the residue of my estate in equal shares to Grace Mendel and Erik Lindqvist.", "Signed March 4, 2019, in the presence of two witnesses."] ] },
  { id: "d-term", matterId: "m5", name: "Termination letter", kind: "letter", added: "2026-09-10", by: "u-ayo", pages: [
    ["NORTHGATE LOGISTICS INC.", "August 29, 2026", "Dear Ms. Haile,", "Your employment as Warehouse Supervisor is terminated effective today, without cause.", "You will receive two weeks' pay in lieu of notice as required by the Employment Standards Act, 2000.", "Your start date was July 12, 2020. Benefits end September 30, 2026.", "Please return your access card."] ] },
  { id: "d-incident", matterId: "m3", name: "Incident report", kind: "report", added: "2026-10-05", by: "u-ayo", pages: [
    ["LAKESHORE TRANSIT CORP. - INCIDENT REPORT", "Date of incident: August 14, 2026, approx. 8:10 a.m.", "Location: Platform 2, Humber Bay station", "Description: passenger slipped on wet surface near stairwell; no wet-floor sign in place.", "Staff on duty: R. Singh. Cleaning log for 7:00-9:00 a.m. not completed."] ] },
  { id: "d-lease", matterId: "m6", name: "Lease renewal draft", kind: "contract", added: "2026-07-28", by: "u-yoni", pages: [
    ["LEASE RENEWAL AGREEMENT", "Tenant: Harbourview Bakery Inc. Landlord: Queensway Plaza Holdings Ltd.", "Term: 10 years from November 1, 2026.", "Base rent: $6,850 per month, increasing 3% per year.", "Rent abatement: none for the period of roof repairs in 2025."],
    ["Tenant is responsible for HVAC repairs regardless of cause.", "Landlord may relocate tenant on 60 days' notice.", "Personal guarantee required from the principal of the tenant."] ] },
];
