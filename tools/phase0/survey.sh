#!/usr/bin/env bash
# Phase 0 survey of a FreePBX/Asterisk host for the support-quality refactor.
#
#   sudo bash survey.sh [--day YYYY-MM-DD]
#
# Read only: SELECT/SHOW queries, `asterisk -rx` show commands, file
# listings. Writes /tmp/asterisk-phase0/ and /tmp/asterisk-phase0.tgz.
#
# Personal data stays on the host: phone numbers longer than 5 digits are
# replaced by a salted hash (the same number maps to the same token within
# one run, so callbacks stay visible; the salt is not written out), caller
# names are dropped, and extension names are not exported.
#
# Environment: CDR_DB (asteriskcdrdb), CONFIG_DB (asterisk), MYSQL (mysql;
# e.g. MYSQL="mysql --defaults-extra-file=/root/.my.cnf").
set -uo pipefail
CDR_DB="${CDR_DB:-asteriskcdrdb}" CONFIG_DB="${CONFIG_DB:-asterisk}" MYSQL="${MYSQL:-mysql}"
day="$(date -d yesterday +%F)"
[ "${1:-}" = --day ] && day="$2"
out=/tmp/asterisk-phase0
rm -rf "$out" && mkdir -p "$out" && cd "$out" || exit 1
salt="$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"

q() { $MYSQL --batch --raw -e "SET @salt='$salt'; $2" "$1" 2>&1; }
section() { printf '\n## %s\n' "$1" >>report.txt; }
run() { section "$1"; shift; "$@" >>report.txt 2>&1 || echo "(failed: $?)" >>report.txt; }
# A number longer than 5 digits (external party) becomes n<8 hex>; extensions stay.
mask() { echo "IF(CHAR_LENGTH($1)>5, CONCAT('n',LEFT(SHA2(CONCAT(@salt,$1),256),8)), $1)"; }
# Digit runs of 6+ inside free text (dial strings, app data) become #.
scrub() { echo "REGEXP_REPLACE(COALESCE($1,''),'[0-9]{6,}','#')"; }

echo "# asterisk phase 0 survey, $(date -Is), sample day $day" >report.txt
run "versions" bash -c 'asterisk -V; fwconsole -V 2>/dev/null || true; '"$MYSQL"' -V; uname -r'
run "cdr modules" asterisk -rx "module show like cdr"
run "cel modules" asterisk -rx "module show like cel"
run "cdr status" asterisk -rx "cdr show status"
run "cel status" asterisk -rx "cel show status"
run "pjsip endpoints (count)" bash -c 'asterisk -rx "pjsip show endpoints" | grep -c "^ Endpoint:"'

# FreePBX configuration
run "ring groups" q "$CONFIG_DB" "SELECT grpnum, strategy, grptime, $(scrub grplist) AS members, $(scrub postdest) AS failover, COALESCE(needsconf,'') AS confirm, COALESCE(cwignore,'') AS cwignore FROM ringgroups ORDER BY grpnum"
run "inbound routes" q "$CONFIG_DB" "SELECT IF(extension='','(any)',$(mask extension)) AS did, $(scrub destination) AS destination FROM incoming ORDER BY destination"
run "outbound routes" q "$CONFIG_DB" "SELECT route_id, name FROM outbound_routes ORDER BY seq"
run "trunks" q "$CONFIG_DB" "SELECT trunkid, tech, channelid, disabled FROM trunks"
run "follow-me" q "$CONFIG_DB" "SELECT grpnum, strategy, grptime, $(scrub grplist) AS members FROM findmefollow WHERE grplist<>'' ORDER BY grpnum"
run "extensions (count by tech)" q "$CONFIG_DB" "SELECT tech, COUNT(*) FROM devices GROUP BY tech"
run "freepbx settings of interest" q "$CONFIG_DB" "SELECT keyword, value FROM freepbx_settings WHERE keyword IN ('CDRDBNAME','CDRDBTYPE','MIXMON_DIR','MIXMON_FORMAT','CEL_ENABLED','TRANSFER_CONTEXT') ORDER BY keyword"

# CDR
run "cdr columns" q "$CDR_DB" "SHOW COLUMNS FROM cdr"
run "cdr size and range" q "$CDR_DB" "SELECT COUNT(*) AS rows_total, MIN(calldate), MAX(calldate) FROM cdr"
run "cdr rows per month" q "$CDR_DB" "SELECT DATE_FORMAT(calldate,'%Y-%m') AS month, COUNT(*) AS legs, COUNT(DISTINCT linkedid) AS calls FROM cdr WHERE calldate>=NOW()-INTERVAL 6 MONTH GROUP BY month ORDER BY month"
run "cdr quality and recording fill (30 days)" q "$CDR_DB" "SELECT COUNT(*) AS legs, SUM(COALESCE(rtpqos,'')<>'') AS with_rtpqos, SUM(COALESCE(peerrtpqos,'')<>'') AS with_peerrtpqos, SUM(COALESCE(recordingfile,'')<>'') AS with_recording FROM cdr WHERE calldate>=NOW()-INTERVAL 30 DAY"
run "cdr contexts (30 days)" q "$CDR_DB" "SELECT dcontext, lastapp, COUNT(*) AS legs FROM cdr WHERE calldate>=NOW()-INTERVAL 30 DAY GROUP BY dcontext, lastapp ORDER BY legs DESC LIMIT 40"
run "cdr dispositions (30 days)" q "$CDR_DB" "SELECT disposition, COUNT(*) FROM cdr WHERE calldate>=NOW()-INTERVAL 30 DAY GROUP BY disposition"
run "cdr channel kinds (30 days)" q "$CDR_DB" "SELECT SUBSTRING_INDEX(channel,'-',1) AS chan, COUNT(*) AS legs FROM cdr WHERE calldate>=NOW()-INTERVAL 30 DAY GROUP BY chan ORDER BY legs DESC LIMIT 60"
run "legs per call (30 days)" q "$CDR_DB" "SELECT legs, COUNT(*) AS calls FROM (SELECT linkedid, COUNT(*) AS legs FROM cdr WHERE calldate>=NOW()-INTERVAL 30 DAY GROUP BY linkedid) t GROUP BY legs ORDER BY legs"
run "database events (purge jobs)" q "$CDR_DB" "SHOW EVENTS"

# CEL
run "cel columns" q "$CDR_DB" "SHOW COLUMNS FROM cel"
run "cel size and range" q "$CDR_DB" "SELECT COUNT(*), MIN(eventtime), MAX(eventtime) FROM cel"
run "cel event types (7 days)" q "$CDR_DB" "SELECT eventtype, COUNT(*) FROM cel WHERE eventtime>=NOW()-INTERVAL 7 DAY GROUP BY eventtype ORDER BY 2 DESC"

# Recordings and retention
run "recordings on disk" bash -c 'd=/var/spool/asterisk/monitor; du -sh "$d"; find "$d" -type f | wc -l; find "$d" -type f -printf "%TY-%Tm\n" | sort | uniq -c | tail -24'
run "cron jobs touching cdr or recordings" bash -c 'grep -rIh -E "cdr|monitor|recording" /etc/cron* /var/spool/cron 2>/dev/null | grep -v "^#" | head -20'

# Golden sample: every call of the sample day, anonymised.
section "golden sample"
q "$CDR_DB" "SELECT linkedid, uniqueid, COALESCE(sequence,0) AS sequence, calldate, channel, dstchannel, $(mask src) AS src, $(mask dst) AS dst, dcontext, lastapp, $(scrub lastdata) AS lastdata, disposition, duration, billsec, COALESCE(rtpqos,'') AS rtpqos, COALESCE(peerrtpqos,'') AS peerrtpqos, COALESCE(recordingfile,'')<>'' AS recorded FROM cdr WHERE calldate>='$day' AND calldate<'$day'+INTERVAL 1 DAY ORDER BY calldate, sequence" >"cdr-$day.tsv"
q "$CDR_DB" "SELECT id, eventtype, eventtime, $(mask cid_num) AS cid_num, $(mask cid_ani) AS cid_ani, $(mask cid_dnid) AS cid_dnid, $(mask exten) AS exten, context, channame, appname, $(scrub appdata) AS appdata, uniqueid, linkedid, peer, $(scrub extra) AS extra FROM cel WHERE eventtime>='$day' AND eventtime<'$day'+INTERVAL 1 DAY ORDER BY eventtime, id" >"cel-$day.tsv"
wc -l "cdr-$day.tsv" "cel-$day.tsv" >>report.txt

cd /tmp && tar czf asterisk-phase0.tgz asterisk-phase0 && chmod 600 asterisk-phase0.tgz
echo "done: /tmp/asterisk-phase0.tgz ($(du -h asterisk-phase0.tgz | cut -f1)); summary in $out/report.txt"
