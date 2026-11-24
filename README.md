# Rule 37 ITC reversal tracker — the 180-day supplier payment deadline

A single-binary tool for a GST-registered business in **India** that has claimed input tax credit on purchases bought on credit. It puts every purchase invoice on the **180-day clock**, works out the credit that must be reversed if the supplier is not paid in time, names the return the reversal lands in, and ranks the payments that protect the most credit for the cash available.

It runs on your own machine, serves a small web interface on `127.0.0.1`, keeps everything in one local JSON file, and makes no outbound connections of any kind. It never contacts a supplier, never touches the GST portal, and files nothing.

## What it does

Give it a CSV of your purchase register and, optionally, a CSV of payments made to suppliers. For each invoice it computes:

- **day 180** — the last day the supplier can be paid, counted from the invoice date;
- **what was unpaid on that day**, and therefore **the credit to be reversed**, in proportion to the unpaid part, head by head across IGST, CGST, SGST/UTGST and cess;
- **the return the reversal belongs in** — the FORM GSTR-3B for the tax period *immediately following* the one the 180 days expire in — and the day it falls due;
- **the re-claim** available once the supplier is paid, in the return for the period of payment.

It then shows the four figures that decide what to do:

| Figure | What it means |
| --- | --- |
| **Credit you can still protect** | ITC on everything unpaid whose 180 days have not run out, with the cash needed to save all of it |
| **Reverses this period if nothing is paid** | The deadlines falling inside the return period you are in |
| **Must be reversed now** | Deadlines already gone, grouped by the return each belongs in, with overdue returns marked |
| **Whom to pay first** | Given your cash, the payments that protect the most credit per rupee |

It also produces an ageing from the invoice date, a per-supplier rollup, a printable **working paper per return period** stating the GSTR-3B table against every figure, and five CSV exports.

## Who it's for

GST-registered micro, small and medium businesses in India that buy on credit, and the bookkeepers and accountants who prepare their GSTR-3B. The liability builds up quietly: the credit was claimed months ago and already set against output tax, and the reversal lands on a return long after anyone remembers the invoice.

## The regulation it implements

**The second proviso to section 16(2) of the Central Goods and Services Tax Act, 2017, and rule 37 of the Central Goods and Services Tax Rules, 2017.**

Input tax credit on a purchase is conditional. Where the recipient of a supply — other than one on which tax is payable on reverse charge — fails to pay the supplier *the amount towards the value of supply along with tax payable thereon* within **one hundred and eighty days from the date of issue of invoice by the supplier**, an amount equal to the credit availed is added to the recipient's output tax liability, with interest. The third proviso restores the credit on payment.

Rule 37 prescribes the manner. It was **substituted with effect from 1 October 2022** by Notification No. 19/2022-Central Tax dated 28 September 2022, and amended by Notification No. 26/2022-Central Tax dated 26 December 2022 with retrospective effect from the same date. This tool implements that current version, under which rule 37(1) requires the registered person to

> …pay or reverse an amount equal to the input tax credit availed in respect of such supply, proportionate to the amount not paid to the supplier, along with interest payable thereon under section 50, while furnishing the return in FORM GSTR-3B for the tax period immediately following the period of one hundred and eighty days from the date of the issue of the invoice.

Three phrases do the work:

- **"pay or reverse"** — the credit comes back out as a reversal in the return, not as a cash demand.
- **"proportionate to the amount not paid"** — a part payment reverses a part of the credit. Those words came with the December 2022 notification; older spreadsheets treating this as all-or-nothing get partial payments wrong.
- **"the tax period immediately following"** — the period *after* the one day 180 falls in, which is not the period the 181st day falls in. They coincide only when day 180 is the last day of a period, so one day of invoice date can move a reversal a whole return.

```
day 180            = invoice date + 180 days
unpaid on day 180  = invoice total - payments reaching the supplier by then
reversal per head  = ITC availed on that head x unpaid on day 180 / invoice total
reversal return    = the period after the one day 180 falls in
re-claim           = the same proportion, restored in the return for the period you pay in
```

Rule 37(2) entitles you to take the credit again on paying the supplier, and **rule 37(4)** — untouched by the substitution — keeps the section 16(4) time limit out of the way, so a supplier paid two years late still restores the credit. Two provisos to rule 37(1) switch the clock off for **Schedule I** supplies made without consideration and for amounts added under **section 15(2)(b)**, both deemed paid.

Per **Circular No. 170/02/2022-GST dated 6 July 2022** the reversal is reported in **table 4(B)(2)** of FORM GSTR-3B and the re-claim in **table 4(A)(5)**, disclosed in **table 4(D)(1)**. The table matters: a rule 37 reversal reported in table 4(B)(1) instead is treated as permanent, never reaches the portal's Electronic Credit Reversal and Re-claimed Statement, and leaves no balance to re-claim against.

### This is not the other two clocks

Three rules can fire on the same fact — an unpaid supplier — and they have nothing else in common:

- **This tool — did *you* pay?** CGST rule 37. You claimed credit and have not paid within 180 days of the invoice date. You reverse your own credit in your own GSTR-3B, and take it back on payment.
- **Did the *supplier* file?** CGST s.16(2)(aa) and rule 36(4). A different failure: the invoice is missing from GSTR-2B because the supplier never reported it, so the credit was never available. See [gstr2b-itc-reconciler](https://github.com/sreenivas-sadhu-prabhakara/gstr2b-itc-reconciler).
- **Does the supplier get interest?** MSMED Act 2006, s.15 and s.16. A different statute, a 45-day limit running from acceptance rather than the invoice date, and a consequence falling the other way — compound interest payable to a micro or small supplier. See [msme-payment-clock-samadhaan](https://github.com/sreenivas-sadhu-prabhakara/msme-payment-clock-samadhaan).

One unpaid invoice can therefore be 45 days from statutory interest under one Act and 180 days from losing input tax credit under another, on two different start dates. This tool is the 180-day one, and only that.

## Install & run

One Go program, standard library only. Nothing to install, nothing to configure.

```sh
git clone https://github.com/sreenivas-sadhu-prabhakara/rule-37-180-day-itc.git
cd rule-37-180-day-itc

# build a single binary
go build -o rule-37-180-day-itc .
./rule-37-180-day-itc

# or just run it
go run .
```

Then open `http://127.0.0.1:8080/`. Two flags change the defaults:

```sh
./rule-37-180-day-itc -addr 127.0.0.1:9000 -data ~/gst/rule-37-data.json
```

### The files it reads

Export your purchase register as a CSV. Headings are read flexibly — the usual spellings used by spreadsheets and accounting packages are understood, title rows above the headings are skipped, a total row at the foot is ignored, and comma, semicolon, tab and pipe separated files all read.

```
Supplier GSTIN,Supplier name,Invoice no,Invoice date,Taxable value,IGST,CGST,SGST,Cess,Invoice value,Amount paid,Payment date,Out-of-scope reason
27AAPFU0939F1ZV,Sundar Metals,SM/001/26-27,15-07-2026,100000.00,18000.00,0,0,0,118000.00,0,,
29AABCU9603R1ZM,Nandi Hardware,NH-0045,24-11-2026,100000.00,0,9000.00,9000.00,0,118000.00,50000.00,01-12-2026,
24AACCG0527D1ZY,Ganga Transport,GT/900,15-07-2026,50000.00,2500.00,0,0,0,52500.00,-,-,reverse-charge
```

Only an **invoice number** and an **invoice date** are required on each row — the date because the 180 days are counted from it. Every other figure may be blank and counts as nil. A row that cannot be used is reported with its line number and left out, and the rest of the file still imports.

Payments can go in the register itself, in an `Amount paid` column with a `Payment date` beside it, or in a second file of one payment per line, matched to the invoice by supplier GSTIN and invoice number:

```
Supplier GSTIN,Invoice no,Payment date,Amount,UTR no
27AAPFU0939F1ZV,SM/001/26-27,10-02-2027,118000.00,UTR2027021000001
29AABCU9603R1ZM,NH-0045,05-01-2027,68000.00,UTR2027010500042
```

Give the payment dates. A paid amount with no date can only be read as "paid by today", which means the 180-day test has to treat it as late — the tool says so, but the date is what makes the answer right. Both sample files above ship in `internal/rule37/testdata/`.

Invoices can also be entered one at a time in the interface, and payments recorded against any invoice.

## A worked example

A ₹1,18,000 invoice dated 15 July 2026 — ₹1,00,000 of goods and ₹18,000 of integrated tax — on which the full credit was taken.

- Day 180 is **11 January 2027**, which falls in the January 2027 return period, so the reversal belongs in the **February 2027** GSTR-3B, due **20 March 2027**.
- Pay nothing and **₹18,000** reverses in table 4(B)(2) of that return.
- Pay ₹50,000 by 11 January, leaving ₹68,000 unpaid, and **₹10,372.88** reverses — ₹18,000 × ₹68,000 ÷ ₹1,18,000, carried into the return as ₹10,373.
- Pay the whole ₹1,18,000 on 10 February 2027 instead and you still report the ₹18,000 reversal in the February return, and take the same ₹18,000 back in table 4(A)(5) of that same return. Net nil, but both entries must appear.
- Miss that return and reverse only in June: roughly **₹816** of indicative interest for 92 days at 18% a year.

And the part that changes behaviour: ₹1,00,000 of cash paid against a **28%** purchase protects ₹21,875 of credit, while the same money against a **5%** purchase protects ₹4,762. Because a part payment protects its own proportion, ranking by credit-per-rupee is the provably best use of limited cash rather than a rule of thumb — and it is rarely the order a payables ageing suggests.

## The figures used, and what you can change

Every statutory figure is a named constant in `internal/rule37/rules.go` with the provision it comes from written beside it, and every one can be changed in the interface.

| Figure | Authority | Default |
| --- | --- | --- |
| Payment window | 2nd proviso to s.16(2); rule 37(1) | **180 days** from the invoice date |
| Reversal return | rule 37(1) | the period **immediately following** the one day 180 falls in |
| GSTR-3B due day, monthly | s.39; rule 61(1)(i) | **20th** of the following month |
| GSTR-3B due day, QRMP | rule 61(1)(ii) | **22nd** or **24th** of the month after the quarter, by State |
| Interest rate | s.50(3); Notification 13/2017-CT as amended by s.116 of the Finance Act, 2022 | **18%** a year (the Act caps the notifiable rate at 24%) |
| Year length for interest | rule 88B | **365 days** |
| Rounding for the return | s.170 | nearest rupee, head by head |
| Ageing bands | a convention of this tool | 0–60, 61–120, 121–150, 151–180, over 180 days |

Further notes on how the arithmetic is done:

- **The 180 days are counted from the day after the invoice date**, so day 180 is the invoice date plus 180 days. This is a *convention*, not something the rule spells out: it follows s.9 of the General Clauses Act, 1897, under which "from" excludes the first day, and it is what practice does. Counting the invoice date as day one makes every deadline a day earlier. It changes the answer only where day 180 falls at a period boundary, and the computed date is shown against every invoice.
- **Whether any interest arises on a *timely* reversal is unsettled.** The pre-October-2022 rule 37(3), which ran interest from the date the credit was availed, was omitted by the same notification that substituted the rest of the rule. What is left says only "interest payable thereon under section 50", and s.50(3) now reaches credit "wrongly availed *and utilised*", with rule 88B(3) running the clock from the date of utilisation — which, by its Explanation, is when the electronic credit ledger balance falls below the credit in question. There is a respectable view that a reversal made in the correct return attracts none, and practitioners who would run interest from availment. No circular settles it. This tool charges interest only on a reversal made *after* the return it belonged in fell due, labels it indicative, and lets you set the rate to nil for the first view or switch the basis to the wider one to see the larger exposure. Decide it with your practitioner.
- **Out of scope:** supplies on which tax is payable on reverse charge (excluded by the proviso itself), Schedule I supplies made without consideration and amounts added under s.15(2)(b) (both deemed paid by the provisos to rule 37(1)), and purchases on which no credit was taken. Mark them in the register and they are counted out with the reason shown.
- **A re-claim is never scheduled in a return earlier than its own reversal.** A quarterly filer who pays soon after day 180 can have the payment fall in the quarter the 180 days expired in while the reversal belongs in the quarter after; the portal's reversal statement would reject a re-claim with no balance behind it, so such a payment is carried forward to the reversal's own return, where the two cancel.
- All amounts are held in **whole paise** in integers, so totals are exact and never drift. Proportions are computed in arbitrary precision and rounded half away from zero, head by head.
- An invoice total left blank is taken as the taxable value plus the credit heads. Where you took less credit than the supplier charged, enter the real invoice total: the proportion is computed against what you owe the supplier, not against the credit.

## What it stores

One JSON file on your own computer, `./rule-37-itc-data.json` by default, holding your settings and your invoices with their payments. It is written atomically — to a temporary file in the same directory that is then renamed — so an interrupted write cannot corrupt it, and it is created readable only by you. A file that exists but cannot be parsed stops the tool with a message rather than being overwritten.

Nothing else leaves the machine. The tool binds to **127.0.0.1**, makes no outbound network calls of any kind, has no accounts, no analytics and no telemetry, and loads no fonts, scripts or styles from anywhere but itself. Delete that one file and the tool knows nothing.

## Scope and accuracy

This tool works out figures from files you give it. **It is not tax advice, it is not a return, and it files nothing.** It cannot tell you whether a credit was eligible in the first place — that also turns on receipt of the goods or services, the supplier having reported and paid the tax, blocked credits and the time limit for claiming — and it knows nothing about debit notes, credit notes, imports or anything absent from the file.

Rule 37 was substituted with effect from 1 October 2022 and this implements that version; the position before that date was materially different and is not implemented. Statutory provisions are cited as they stood when this was written, and the validity of the 180-day condition is itself reported to be under challenge before a High Court, with nothing stayed and the rule fully in force. Rates, dates and rules change. Check every figure against your own invoices and ledger, and confirm the current law with the GST portal or a qualified practitioner, before you file.

## Tests

The rules live in `internal/rule37` as pure functions over values — nothing in that package reads a clock, a file or a network — so the arithmetic is tested on fixed dates:

```sh
gofmt -l .
go vet ./...
go test ./... -count=1
go build -o rule-37-180-day-itc .
```

The table-driven tests cover day-180 arithmetic across leap days and month ends, the reversal return period for monthly and quarterly filers, partial payments and their rounding at both ends of the range, re-claims split across returns, interest on both bases, the ageing boundaries from either side, the payment plan including its partial tail and tie-breaks, and a purchase register full of rows that cannot be used.

MIT licensed.
