# epoch reads an RFC 3339 time as seconds, keeping its fraction and its offset, since Go writes
# nanoseconds and jq's fromdateiso8601 reads whole seconds in UTC alone.
def epoch:
  (capture("^(?<d>[0-9-]+T[0-9:]+)(\\.(?<f>[0-9]+))?(?<z>Z|[+-][0-9]{2}:[0-9]{2})$")
    // error("not an RFC 3339 time: \(.)"))
  | ((.d + "Z") | fromdateiso8601) + (("0." + (.f // "0")) | tonumber)
    - (if .z == "Z" then 0
       else ((.z[0:1] + "1") | tonumber) * ((.z[1:3] | tonumber) * 3600 + (.z[4:6] | tonumber) * 60)
       end);
