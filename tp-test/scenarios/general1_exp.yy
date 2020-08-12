{

    local util = require("util")

    T = {
        c_int = { seq = util.seq(10) },
        c_str = {},
        c_datetime = { range = util.range(1577836800, 1593561599) },
        c_timestamp = { range = util.range(1577836800, 1593561599) },
        c_double = { range = util.range(100) },
        c_decimal = { range = util.range(10) },
    }

    T.c_int.rand = function() return T.c_int.seq:rand() end
    T.c_str.rand = function() return random_name() end
    T.c_datetime.rand = function() return T.c_datetime.range:randt() end
    T.c_timestamp.rand = function() return T.c_timestamp.range:randt() end
    T.c_double.rand = function() return T.c_double.range:randf() end
    T.c_decimal.rand = function() return T.c_decimal.range:randf() end

}

init: create_table; insert_data

txn: rand_queries

create_table:
    create table t (
        c_int int,
        c_str varchar(40),
        c_datetime datetime,
        c_timestamp timestamp,
        c_double double,
        c_decimal decimal(12, 6)
        key_primary
        key_c_int
        key_c_str
        key_c_decimal
        key_c_datetime
        key_c_timestamp
    )

key_primary:
 |  , primary key(c_int)
 |  , primary key(c_str)
 |  , primary key(c_int, c_str)

key_c_int:
 |  , key(c_int)
 |  , unique key(c_int)

key_c_str:
 |  , key(c_str)
 |  , unique key(c_str)

key_c_decimal:
 |  , key(c_decimal)
 |  , unique key(c_decimal)

key_c_datetime:
 |  , key(c_datetime)
 |  , unique key(c_datetime)

key_c_timestamp:
 |  , key(c_timestamp)


insert_data:
    insert into t values next_row, next_row, next_row, next_row, next_row;
    insert into t values next_row, next_row, next_row, next_row, next_row;
    insert into t values next_row, next_row, next_row, next_row, next_row;

next_row: (next_c_int, rand_c_str, rand_c_datetime, rand_c_timestamp, rand_c_double, rand_c_decimal)
rand_row: (rand_c_int, rand_c_str, rand_c_datetime, rand_c_timestamp, rand_c_double, rand_c_decimal)

next_c_int: { print(T.c_int.seq:next()) }
rand_c_int: { print(T.c_int.rand()) }
rand_c_str: { printf("'%s'", T.c_str.rand()) }
rand_c_str_or_null: rand_c_str | [weight=0.2] null
rand_c_datetime: { printf("'%s'", T.c_datetime.rand()) }
rand_c_timestamp: { printf("'%s'", T.c_timestamp.rand()) }
rand_c_double: { printf("%.6f", T.c_double.rand()) }
rand_c_decimal: { printf("%.3f", T.c_decimal.rand()) }

union_or_union_all: union | union all
insert_or_replace: insert | replace

rand_queries:
    rand_query; rand_query; rand_query; rand_query
 |  [weight=9] rand_query; rand_queries

rand_query:
    [weight=0.3] common_select maybe_for_update
 |  [weight=0.2] (common_select maybe_for_update) union_or_union_all (common_select maybe_for_update)
 |  [weight=0.3] agg_select maybe_for_update
 |  [weight=0.2] (agg_select maybe_for_update) union_or_union_all (agg_select maybe_for_update)
 |  [weight=0.5] common_insert
 |  common_update
 |  common_delete
 |  common_update; common_delete; common_select
 |  common_insert; common_delete; common_select
 |  common_delete; common_insert; common_update
 |  rollback;

maybe_for_update: | for update
maybe_write_limit: | [weight=2] order by c_int, c_str, c_double, c_decimal, c_datetime, c_timestamp limit { print(math.random(3)) }

col_list: c_int, c_str, c_double, c_decimal, c_datetime, c_timestamp

common_select:
    select col_list maybe_col_exps from t where expr order by col_list

maybe_col_exps: 
  |, complex_numeric_col_exprs

agg_select:
    select count(*) from t where c_timestamp between { t = T.c_timestamp.rand(); printf("'%s'", t) } and date_add({ printf("'%s'", t) }, interval 15 day)
 |  select sum(c_int) from t where c_datetime between { t = T.c_datetime.rand(); printf("'%s'", t) } and date_add({ printf("'%s'", t) }, interval 15 day)
 |  select agg_exp from t where expr

common_update:
    update t set c_str = rand_c_str where c_int = rand_c_int
 |  update t set c_double = c_decimal, c_decimal = rand_c_decimal where c_int in (rand_c_int, rand_c_int, rand_c_int)
 |  update t set c_datetime = c_timestamp, c_timestamp = rand_c_timestamp where c_str in (rand_c_str_or_null, rand_c_str_or_null, rand_c_str_or_null)
 |  update t set c_int = c_int + 10, c_str = rand_c_str where c_int in (rand_c_int, { local k = T.c_int.seq:head(); print(k-2) })
 |  update t set c_int = c_int + 5, c_str = rand_c_str_or_null where (c_int, c_str) in ((rand_c_int, rand_c_str), (rand_c_int, rand_c_str), (rand_c_int, rand_c_str))
 |  [weight=0.4] update t set c_datetime = rand_c_datetime, c_timestamp = rand_c_timestamp, c_double = rand_c_double, c_decimal = rand_c_decimal where c_datetime is null maybe_write_limit
 |  [weight=0.4] update t set c_datetime = rand_c_datetime, c_timestamp = rand_c_timestamp, c_double = rand_c_double, c_decimal = rand_c_decimal where c_decimal is null maybe_write_limit

common_insert:
    insert into t values next_row
 |  [weight=0.5] insert_or_replace into t values next_row, next_row, ({ print(T.c_int.seq:head()-1) }, rand_c_str, rand_c_datetime, rand_c_timestamp, rand_c_double, rand_c_decimal)
 |  insert_or_replace into t (c_int, c_str, c_datetime, c_double) values (rand_c_int, rand_c_str, rand_c_datetime, rand_c_double)
 |  insert_or_replace into t (c_int, c_str, c_timestamp, c_decimal) values (next_c_int, rand_c_str, rand_c_timestamp, rand_c_decimal), (rand_c_int, rand_c_str, rand_c_timestamp, rand_c_decimal)
 |  insert into t values rand_row, rand_row, next_row on duplicate key update c_int=values(c_int), c_str=values(c_str), c_double=values(c_double), c_timestamp=values(c_timestamp)
 |  insert into t values rand_row, rand_row, next_row on duplicate key update c_int = c_int + 1, c_str = concat(c_int, ':', c_str)

common_delete:
    delete from t where c_int = rand_c_int
 |  delete from t where c_int in ({ local k = T.c_int.seq:head(); print(k-2) }, rand_c_int) or c_str in (rand_c_str, rand_c_str, rand_c_str, rand_c_str) maybe_write_limit
 |  delete from t where c_str is null
 |  delete from t where c_decimal > c_double/2 maybe_write_limit
 |  [weight=0.8] delete from t where c_timestamp is null or c_double is null maybe_write_limit

expr:
    (expr) OR (expr)
  | (expr) XOR (expr)
  | (expr) AND (expr)
  | (expr) && (expr)
  |  NOT (expr)
  | !(expr)
  | [weight=20] (boolean_primary) IS maybe_not true_or_false
  | [weight=20] (boolean_primary)

boolean_primary:
    (boolean_primary) IS maybe_not NULL
  | (boolean_primary) <=> (boolean_primary)
  | (boolean_primary) comparison_operator (boolean_primary)
  | [weight=10] numeric_expr comparison_operator numeric_expr
  | [weight=5] datetime_expr comparison_operator datetime_expr
  | [weight=5] time_expr comparison_operator time_expr
  | predicate

comparison_operator: = | >= | > | <= | < | <> | !=

predicate:
    numeric_expr maybe_not BETWEEN numeric_expr AND numeric_expr
  | datetime_expr maybe_not BETWEEN datetime_literal AND datetime_literal
    

complex_numeric_col_exprs:
    [weight=2] complex_numeric_col_expr | complex_numeric_col_expr, complex_numeric_col_exprs

complex_numeric_col_expr:
    numeric_col_expr numeric_operator numeric_col_expr
  | rand_c_int numeric_operator numeric_col_expr
  | numeric_col_expr numeric_operator rand_c_int


numeric_col_expr:       
    numeric_col_expr numeric_operator numeric_col_expr
  | rand_c_int numeric_operator numeric_col_expr
  | numeric_col_expr numeric_operator rand_c_int
  | [weight=4] c_int
  

numeric_expr:
    numeric_expr numeric_operator numeric_expr
  | [weight=4] simple_expr
  | ascii_expr
  | bitlength_expr
  | length_expr

numeric_operator: + | - | * | DIV | MOD | %

simple_expr:
    numeric_literal
  | [weight=3] numeric_col

agg_exps: 
    agg_exp
  | agg_exp, agg_exps

agg_exp:
    [weight=5] ROUND(numeric_agg_func_name(numeric_col), 3)
  | COUNT(DISTINCT any_col)
  | ROUND(SUM(DISTINCT numeric_col), 3)

agg_operand: 
    [weight=2] numeric_col 
  | *

numeric_literal: rand_c_int | rand_c_double | rand_c_decimal

numeric_col: t.c_int | t.c_double | t.c_decimal

any_col: c_int | c_str | c_double | c_decimal | c_datetime | c_timestamp

numeric_agg_func_name: AVG | MAX | MIN | SUM | COUNT

maybe_not: | NOT

true_or_false: TRUE | FALSE

datetime_expr:
    [weight=5] datetime_literal
  | c_datetime
  | [weight=0] date

adddate_expr: 
    ADDDATE(date_expr, INTERVAL numeric_expr unit)
  | ADDDATE(date_expr, numeric_expr) 

date: 
    date_expr
  | datediff_expr

date_expr: 
    [weight=3] date_literal
  | [weight=0.5] DATE(datetime_expr)
  | adddate_expr

date_literal:
    [weight=0.1] CURDATE()
  | [weight=0.1] CURRENT_DATE()
  | [weight=0.1] CURRENT_DATE

datediff_expr: DATEDIFF(datetime_expr, datetime_expr)

datetime_literal: 
    rand_c_datetime

unit: 
    MICROSECOND
  | SECOND
  | MINUTE
  | HOUR
  | DAY
  | WEEK
  | MONTH
  | QUARTER
  | YEAR

time_expr: 
    [weight=4] time_literal
  | addtime_expr
  | [weight=0.3] converttz_expr
  | [weight=0] timestamp_expr
  | TIME(timestamp_expr)
  | TIME(datetime_expr)

addtime_expr: ADDTIME(time_expr, time_expr)

converttz_expr: CONVERT_TZ(time_expr, timezone_expr, timezone_expr)

timezone_expr: {print("'")}plus_or_minus timezone_number minutes

minutes: 
    {print(":00'")}
  | {print(":30'")}
  | {print(":13'")}
  | {print(":41'")}

plus_or_minus: + | -

timezone_number: 00 | 01 | 02 | 03 | 04 | 05 | 06 | 07 | 08 | 09 | 10 | 11 | 12

time_literal: 
    CURTIME()
  | CURRENT_TIME()
  | CURRENT_TIME

timestamp_expr: 
    c_timestamp
  | NOW()
  | rand_c_timestamp

str_exprs:
    str_expr
  | str_expr, str_exprs

str_expr: 
    [weight=5] str_literal
  | concat_expr
  | [weight=5]c_str
  | bin_exp

concat_expr:
    CONCAT(str_exprs)

str_literal:
    rand_c_str

ascii_expr: 
    ASCII(str_expr)

bin_exp:
    BIN(numeric_expr)

bitlength_expr:
    BIT_LENGTH(str_expr)

length_expr:
    LENGTH(str_expr)